package delve

import (
	"context"
	"log"
	rtdebug "runtime/debug"

	"github.com/go-delve/delve/service/api"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
)

// Running the program: the four controls, and the one goroutine that waits for
// it to stop.
//
// Delve's Command BLOCKS until the debuggee stops, which is the shape this file
// is built around. Every resume runs on a goroutine of its own and reports
// through the watcher when it comes back — a window that called Command on the
// frame thread would freeze for as long as the program ran.

// SetBreakpoints replaces the breakpoints in one file.
//
// Replaces, like DAP's own request and for the same reason: the window keeps
// the set and sends it whole, so a breakpoint removed in the editor is removed
// here by not being in the list. Delve has no such call, so this clears the
// file's breakpoints and creates them again — which is also what makes an
// edited condition take effect.
func (c *Client) SetBreakpoints(_ context.Context, file string,
	points []debug.Breakpoint) ([]debug.Breakpoint, error) {

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live == nil {
		return nil, debug.ErrNotDebugging
	}
	c.clearFile(file)
	out := make([]debug.Breakpoint, 0, len(points))
	for _, point := range points {
		out = append(out, c.create(file, point))
	}
	return out, nil
}

// clearFile removes every breakpoint this session has in one file. Called with
// the lock held.
func (c *Client) clearFile(file string) {
	for _, existing := range c.live.Breakpoints(false) {
		if existing.File == file {
			_, _ = c.live.ClearBreakpoint(existing)
		}
	}
}

// create sets one breakpoint, answering what the debugger made of it.
//
// A REFUSED breakpoint comes back unverified carrying Delve's own words rather
// than being dropped: one that vanished is one the operator waits at forever,
// and "could not find statement at main.go:12" is a sentence they can act on.
func (c *Client) create(file string, point debug.Breakpoint) debug.Breakpoint {
	made, err := c.live.CreateBreakpoint(&api.Breakpoint{
		File: file, Line: point.Line,
		Cond: point.Condition, HitCond: point.Hits,
	}, "", nil, false)
	if err != nil {
		point.File, point.Verified, point.Message = file, false, err.Error()
		return point
	}
	point.File, point.Verified, point.Line = file, true, made.Line
	return point
}

// Continue, Step, StepIn and StepOut resume execution.
//
// Four facades over one implementation (Rule 5.2): they differ in the name
// Delve calls the command, and in nothing else.
func (c *Client) Continue(ctx context.Context) error { return c.resume(ctx, api.Continue) }

// Step runs to the next line in this frame.
func (c *Client) Step(ctx context.Context) error { return c.resume(ctx, api.Next) }

// StepIn enters the call on this line.
func (c *Client) StepIn(ctx context.Context) error { return c.resume(ctx, api.Step) }

// StepOut runs until this frame returns.
func (c *Client) StepOut(ctx context.Context) error { return c.resume(ctx, api.StepOut) }

// resume sends one command and waits for the stop on its own goroutine.
//
// A command sent while another is in flight is REFUSED rather than queued.
// Delve's Command blocks until the program stops, so a second one would wait on
// a lock held by the first for as long as the debuggee ran — which from the
// window looks exactly like the editor hanging.
func (c *Client) resume(ctx context.Context, name string) error {
	c.mu.Lock()
	if c.live == nil {
		c.mu.Unlock()
		return debug.ErrNotDebugging
	}
	if c.running {
		c.mu.Unlock()
		return debug.ErrBusy
	}
	c.running = true
	c.mu.Unlock()
	// The handles go with the moment they described: one from the last stop
	// names a value that may not exist any more (refs.go).
	c.forgetRefs()
	c.say(debug.Session{State: debug.Running})
	go c.command(ctx, name)
	return nil
}

// run is the first continue, which lets the launched program go.
func (c *Client) run(ctx context.Context) {
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()
	c.command(ctx, api.Continue)
}

// command drives one Delve command to its stop and reports it.
//
// GUARDED, because Delve runs in this process: a panic inside it would take the
// editor with it, and an editor that dies when a debugger trips over a stripped
// binary is worse than one that cannot debug.
func (c *Client) command(ctx context.Context, name string) {
	defer c.finish()
	state, err := c.send(ctx, name)
	if c.over() {
		return
	}
	switch {
	case err != nil:
		c.say(debug.Session{State: debug.Ended, Err: err})
	case state == nil || state.Exited:
		c.say(exited(state))
	default:
		c.say(stopped(state))
	}
}

// send runs the command under the lock, recovering from anything it does.
//
// The recovered fault is WRITTEN DOWN as well as turned into an error: the
// operator is told the debugger fell over, which is all they can act on, and
// whoever reads the log afterwards needs to know what it fell over on — a
// recover that swallows its own reason is a defect that can only be found by
// guessing (live run, 2026-09-18).
func (c *Client) send(_ context.Context, name string) (state *api.DebuggerState, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("debugkit/delve: %s panicked: %v\n%s", name, r, rtdebug.Stack())
			state, err = nil, debug.ErrDebuggerFailed
		}
	}()
	c.mu.Lock()
	live := c.live
	c.mu.Unlock()
	if live == nil {
		return nil, debug.ErrNotDebugging
	}
	state, err = live.Command(&api.DebuggerCommand{Name: name}, nil, nil, nil)
	c.remember(state)
	return state, err
}

// remember keeps what Delve last said, so an inspection knows which goroutine
// it is asking about.
func (c *Client) remember(state *api.DebuggerState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = state
}

// finish marks the command done, whatever it did.
func (c *Client) finish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = false
}

// stopped is a session at a breakpoint.
//
// The THREAD is the goroutine where there is one, because that is what every
// later question is asked about — a stack trace is per goroutine, and reporting
// the operating system thread would have the window asking about one thing and
// the debugger answering about another.
func stopped(state *api.DebuggerState) debug.Session {
	out := debug.Session{State: debug.Stopped}
	if thread := state.CurrentThread; thread != nil {
		out.Stop = debug.Stop{Thread: thread.ID, Reason: reasonOf(thread), Text: stopText(thread)}
	}
	if g := state.SelectedGoroutine; g != nil {
		out.Stop.Thread = int(g.ID)
	}
	return out
}

// stopText is what the debugger said about the stop, which for a breakpoint
// with a condition on it is the condition that held.
func stopText(thread *api.Thread) string {
	if thread.Breakpoint == nil {
		return ""
	}
	return thread.Breakpoint.Cond
}

// reasonOf says why execution stopped, in the protocol's own vocabulary so the
// window need not learn a second one.
func reasonOf(thread *api.Thread) debug.Reason {
	if thread.Breakpoint != nil {
		return debug.Reason("breakpoint")
	}
	return debug.Reason("step")
}

// exited is a program that finished.
//
// Ended with NO error: a debuggee that exited non-zero is a debugging session
// that worked, and reporting its exit status as a failure of the debugger is
// the confusion `Session.Err` is documented to avoid.
func exited(*api.DebuggerState) debug.Session {
	return debug.Session{State: debug.Ended}
}
