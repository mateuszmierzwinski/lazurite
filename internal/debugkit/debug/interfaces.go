package debug

import "context"

// Debugger runs and controls a debugging session. It is the port an adapter
// fills, and the one an application holds.
//
// Every method takes a context first: a debug adapter is a subprocess that may
// never answer, and a window that waited on one would be frozen with no way
// back (Rule 20.5).
type Debugger interface {
	// Launch starts a program under the debugger. The configuration is the
	// runner's, so a run and a debug of the same thing cannot drift apart.
	Launch(ctx context.Context, program string, args []string, dir string) error

	// SetBreakpoints replaces the breakpoints in one file, which is what DAP
	// itself does — there is no "add one" — and answers with what the debugger
	// ACCEPTED, which may differ from what was asked.
	//
	// It takes BREAKPOINTS rather than lines because a line is not all a
	// breakpoint is: a condition, a hit count and a log message travel with it,
	// and a port that carried only lines would make every one of those a second
	// call that could disagree with the first.
	SetBreakpoints(ctx context.Context, file string, points []Breakpoint) ([]Breakpoint, error)

	// Continue, Step, StepIn and StepOut resume execution.
	Continue(ctx context.Context) error
	Step(ctx context.Context) error
	StepIn(ctx context.Context) error
	StepOut(ctx context.Context) error

	// Frames is the call stack where execution is stopped.
	Frames(ctx context.Context) ([]Frame, error)

	// Variables is what is in scope, one level at a time. A zero ref asks for
	// the current frame's own scope.
	Variables(ctx context.Context, ref int) ([]Variable, error)

	// Evaluate works out an expression IN A FRAME, which is the whole point of
	// it: `err` means something different three frames up, and an evaluation
	// that ignored the selection would answer confidently about the wrong one.
	//
	// It answers a Variable rather than a string, because the result may have
	// children — a struct, a slice — and the pane that shows it expands them
	// through the same Variables call an ordinary variable uses.
	Evaluate(ctx context.Context, expression string, frame int) (Variable, error)

	// Stop ends the session and the debuggee with it. It is safe to call on a
	// session that has already ended, so a window closing need not ask first.
	Stop(ctx context.Context) error
}

// Watcher is told how a session changes.
//
// Called from the adapter's reading goroutine, so an implementation POSTS and
// returns: it must not block, and it must not touch a widget.
type Watcher interface {
	// Changed carries the whole session rather than a delta, because a watcher
	// that reconstructed state from deltas would drift from the adapter after
	// one missed message.
	Changed(s Session)
	// Output is the debuggee's own output, which goes to a terminal tab.
	Output(text string)
}
