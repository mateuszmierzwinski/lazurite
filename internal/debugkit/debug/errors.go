package debug

import "errors"

// The reasons a debugging session cannot proceed, named so a caller can tell
// "there is no debugger on this machine" — a normal state — from "the debugger
// refused", which is worth reporting.
var (
	// ErrNoAdapter is a machine with no debug adapter installed. It is the
	// NORMAL state on a machine that never installed one, and a window meeting
	// it hides its debug controls rather than reporting a fault.
	ErrNoAdapter = errors.New("debug: no debug adapter is installed")

	// ErrNotDebugging is a step or a continue asked for with no live session.
	ErrNotDebugging = errors.New("debug: nothing is being debugged")

	// ErrRunning is an inspection asked for while the debuggee runs. Variables
	// read then describe a moment that has already passed.
	ErrRunning = errors.New("debug: the program is running; stop it first")

	// ErrAdapterGone is a debug adapter that exited mid-session. The session
	// ends rather than appearing to continue against nothing.
	ErrAdapterGone = errors.New("debug: the debug adapter exited")

	// ErrAlreadyDebugging is a second session asked for while one is live. Two
	// debuggers over one window would leave the operator unable to say which
	// program a step was for.
	ErrAlreadyDebugging = errors.New("debug: something is already being debugged")

	// ErrBuildFailed is a program that did not compile.
	//
	// The most ordinary reason a debug session does not start, and the one an
	// operator can act on at once — so it is named, and the compiler's own
	// first lines travel with it. "exit status 1" in the status bar is a
	// debugger telling somebody about a subprocess's return code.
	ErrBuildFailed = errors.New("debug: the program could not be built")

	// ErrBusy is a step asked for while the program is running.
	//
	// It matters for an in-process debugger: Delve's own command blocks until
	// the program stops, so a second one would wait on a lock the first holds
	// for as long as the debuggee runs — which from the window looks exactly
	// like the editor hanging.
	ErrBusy = errors.New("debug: the program is running; wait for it to stop")

	// ErrDebuggerFailed is a debugger that fell over.
	//
	// It exists because the debugger may be LINKED IN: a panic inside it would
	// take the editor with it, so the call is guarded and answers with this
	// instead. An editor that dies because a debugger tripped over a stripped
	// binary is worse than one that cannot debug.
	ErrDebuggerFailed = errors.New("debug: the debugger failed")
)
