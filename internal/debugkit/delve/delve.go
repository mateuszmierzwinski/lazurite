// Package delve debugs a Go program by LINKING DELVE IN, rather than by
// running `dlv` and talking to it.
//
// The operator asked for this directly (2026-09-18): "I would prefer to
// interface it directly to the app (compile in as external module
// dependency)". Three things follow from it, and they are why this package
// exists beside `adapters/dap` rather than replacing it:
//
//   - there is no binary to install and no version to keep in step. A machine
//     with the Go toolchain can debug, which is the same bar the editor already
//     sets for building;
//   - there is no protocol in the middle. A breakpoint is a struct passed to a
//     function, so a field that is dropped is a compile error rather than a
//     silent JSON omission;
//   - and Delve runs INSIDE this process, which is the cost. It ptraces the
//     debuggee from the editor's own address space, so every call here is guarded
//     and nothing it does is allowed to take the window down with it.
//
// `adapters/dap` stays: it is the generic path, and a debug adapter for another
// language is a program that speaks DAP rather than a Go package to import.
package delve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	rtdebug "runtime/debug"
	"strings"
	"sync"

	"github.com/go-delve/delve/pkg/gobuild"
	"github.com/go-delve/delve/service/api"
	"github.com/go-delve/delve/service/debugger"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
)

// Client is one debugging session, or none.
//
// The mutex protects the session and everything that belongs to it. Delve's own
// Debugger is not safe to drive from two goroutines, and this one is driven
// from the frame thread (a step control) and from the goroutine that waits for
// the program to stop.
type Client struct {
	mu      sync.Mutex
	live    *debugger.Debugger
	watcher debug.Watcher

	// built is the binary this package compiled, removed when the session ends
	// — a debug build left in a temporary directory per run is rubbish nobody
	// goes looking for.
	built string

	// state is what Delve last said, so Frames and Variables know which
	// goroutine they are asking about.
	state *api.DebuggerState

	// running says a Command is in flight. Delve's Command BLOCKS until the
	// program stops, so a second one sent while the first is waiting is the
	// deadlock this flag exists to refuse.
	running bool

	// The handles the window expands a variable by (refs.go). A lock of their
	// own, because they are written while a value is being converted and read
	// while the next expansion asks — and neither has anything to do with the
	// debugger's own lock.
	refMu   sync.Mutex
	refs    map[int]reference
	nextRef int

	// waits and captured are the two ways a client can differ (options.go),
	// and streams is what a captured session is reading the program through.
	waits    bool
	captured bool
	streams  []io.ReadCloser
}

// New builds a client that reports to a watcher.
//
// It returns the INTERFACE (Rule 7.2), and it starts nothing: a window with a
// debugger it never uses must cost nothing at all.
func New(watcher debug.Watcher, options ...Option) debug.Debugger {
	client := &Client{watcher: watcher}
	for _, option := range options {
		option(client)
	}
	return client
}

// Launch starts a program under Delve.
//
// The configuration it is given is the RUN configuration — `go run .`, a binary,
// whatever the operator chose — so the first thing this does is work out what
// of that is a Go package and what is a program (target.go). A configuration
// naming neither is refused with a message that says so, rather than launched
// into a failure the operator has to read Delve's own words to understand.
func (c *Client) Launch(ctx context.Context, program string, args []string, dir string) (err error) {
	// GUARDED like every other call into Delve, and for the same reason: it
	// runs in this process. The fault is written down as well as returned —
	// the window can only say that the debugger fell over, and whoever reads
	// the log afterwards needs to know what it fell over on.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("debugkit/delve: launch panicked: %v\n%s", r, rtdebug.Stack())
			err = debug.ErrDebuggerFailed
		}
	}()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live != nil {
		return debug.ErrAlreadyDebugging
	}
	binary, runArgs, err := c.prepare(program, args, dir)
	if err != nil {
		return err
	}
	config := configFor(dir)
	c.redirect(config)
	live, err := debugger.New(config, append([]string{binary}, runArgs...))
	if err != nil {
		c.release()
		return err
	}
	c.live, c.state = live, nil
	c.forgetRefs()
	c.begin(ctx)
	return nil
}

// begin lets a launched program go — or, for a client built Held, says it
// is waiting and leaves the first Continue to the caller. Called with the
// lock held.
func (c *Client) begin(ctx context.Context) {
	if c.waits {
		c.say(debug.Session{State: debug.Starting})
		return
	}
	c.say(debug.Session{State: debug.Running})
	go c.run(ctx)
}

// prepare works out what to debug and builds it where that is a package.
//
// The BUILD is done here rather than left to Delve because what the operator
// chose is a run configuration: `go run .` names a package, and Delve debugs a
// binary. The flags are the debugger's own (`-N -l`, no inlining and no
// optimisation), which is what makes a breakpoint land on the line it was set
// on — gobuild applies them.
func (c *Client) prepare(program string, args []string, dir string) (string, []string, error) {
	target, runArgs, isPackage := targetOf(program, args, dir)
	if target == "" {
		return "", nil, errNotDebuggable
	}
	if !isPackage {
		return target, runArgs, nil
	}
	binary, err := privateBinary()
	if err != nil {
		return "", nil, err
	}
	c.built = binary
	if err := buildIn(dir, binary, target); err != nil {
		c.cleanup()
		return "", nil, err
	}
	return binary, runArgs, nil
}

// privateBinary is where a debug build is written: a file in a directory
// made for it, which only this user can enter.
//
// NOT a name in the shared temporary directory. A name anyone can predict
// there — and one made from the process id is predictable — can be made by
// somebody else first, as a link to a file of the operator's; the build would
// then be written through it. A directory made by MkdirTemp is new, and
// closed to everyone else.
func privateBinary() (string, error) {
	dir, err := os.MkdirTemp("", "debugkit-")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, builtName()), nil
}

// buildIn compiles a package for debugging, IN the project.
//
// `-C dir` and not a working directory of our own: the go command establishes
// the main module from where it runs, and it runs wherever the WINDOW was
// started — which is somebody's home directory, where a build of a package by
// its absolute path answers "go.mod file not found in current directory or any
// parent directory" and reaches the operator as `exit status 1` (live run,
// 2026-09-18). Changing this process's directory instead is not an option: it
// is process-wide, and the editor is reading and writing files on other
// goroutines the whole time.
//
// The build's own OUTPUT is kept and reported. A compiler error is the most
// ordinary reason a debug session does not start, and a status bar reading
// "exit status 1" tells the operator nothing they can act on.
func buildIn(dir, binary, target string) error {
	_, out, err := gobuild.GoBuildCombinedOutput(binary, []string{target}, []string{"-C", dir})
	if err == nil {
		return nil
	}
	if said := strings.TrimSpace(string(out)); said != "" {
		return fmt.Errorf("%w: %s", debug.ErrBuildFailed, firstLines(said))
	}
	return fmt.Errorf("%w: %v", debug.ErrBuildFailed, err)
}

// firstLines is the head of a compiler's output: enough to name the fault,
// short enough for the one line the status bar draws.
func firstLines(said string) string {
	lines := strings.Split(said, "\n")
	if len(lines) > maxBuildLines {
		lines = lines[:maxBuildLines]
	}
	return strings.Join(lines, "; ")
}

// maxBuildLines is how much of a failed build the operator is shown.
const maxBuildLines = 3

// configFor is how Delve is set up for one session.
//
// The backend is DEFAULT, which on Linux is ptrace through the native backend:
// naming one would be this editor choosing for a machine it cannot see. The Go
// version check is ON, because a binary built by another toolchain fails in
// ways that read as the debugger being broken.
func configFor(dir string) *debugger.Config {
	return &debugger.Config{
		WorkingDir:     dir,
		Backend:        "default",
		CheckGoVersion: true,
		ExecuteKind:    debugger.ExecutingExistingFile,
	}
}

// Stop ends the session and the debuggee with it.
//
// Safe on a session that has already ended, so a window closing need not ask
// first — and the built binary goes with it, because a debug build per run left
// in a temporary directory is rubbish nobody goes looking for.
func (c *Client) Stop(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live == nil {
		return nil
	}
	c.halt()
	err := c.live.Detach(true)
	c.live, c.state, c.running = nil, nil, false
	c.release()
	c.say(debug.Session{State: debug.Ended})
	return err
}

// halt stops a program that is running, so that it can be detached from.
//
// Delve's Detach waits for the lock a running Continue holds, and a program
// that never stops by itself — a server, a loop waiting for input — would
// keep it for ever: Stop would hang, holding this client's own lock with it.
// Halt is the one command Delve takes without that lock. Called with the
// lock held.
func (c *Client) halt() {
	if !c.running {
		return
	}
	defer func() { _ = recover() }()
	_, _ = c.live.Command(&api.DebuggerCommand{Name: api.Halt}, nil, nil, nil)
}

// over reports whether the session ended while a command was in flight: its
// stop is then news about a program that is gone.
func (c *Client) over() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live == nil
}

// cleanup removes what this package built. Called with the lock held.
func (c *Client) cleanup() {
	if c.built == "" {
		return
	}
	gobuild.Remove(c.built)
	_ = os.Remove(filepath.Dir(c.built))
	c.built = ""
}

// release lets go of everything a session held besides the debugger itself:
// the binary it built and the streams it was reading. Called with the lock
// held.
func (c *Client) release() {
	c.cleanup()
	c.closeStreams()
}

// say tells the watcher where the session got to.
//
// Called with the lock HELD, and the watcher's contract is that it posts and
// returns — a watcher that called back into this client would deadlock, which
// is why that contract is written on the port rather than assumed here.
func (c *Client) say(session debug.Session) {
	if c.watcher != nil {
		c.watcher.Changed(session)
	}
}

// builtName is a name for the binary this package compiles.
//
// The PID is in it because two windows debugging at once would otherwise build
// over each other's binary — and the second one would debug the first one's
// program while showing the second one's source.
func builtName() string {
	return "debugkit-" + strings.TrimSpace(pidText()) + ".bin"
}

// errNotDebuggable is a run configuration Delve cannot make sense of.
var errNotDebuggable = errors.New(
	"this configuration runs a command rather than a Go program: " +
		"debugging needs a Go package or a built binary")
