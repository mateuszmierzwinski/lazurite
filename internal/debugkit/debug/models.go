// Package debug is the vocabulary of a debugging session: where execution is
// stopped, what is on the stack, and what the values are.
//
// It knows nothing of the Debug Adapter Protocol, of processes or of widgets.
// An adapter speaks DAP and an application shows the result; this says what a
// debugging session IS, so neither imports the other — the same split
// lib/langkit/language draws for the language server.
package debug

// State is what the debuggee is doing.
type State uint8

// The states. Off is the zero value, so a session nobody started reads as not
// running rather than as stopped at a breakpoint nobody set.
const (
	Off State = iota
	Starting
	Running
	Stopped
	Ended
)

// Reason is why execution stopped, as the adapter reported it.
//
// It is kept as the adapter's own string rather than mapped to an enum: a
// debugger may stop for something this package has never heard of, and a reason
// flattened to "other" is one the operator cannot act on.
type Reason string

// Breakpoint is a line the operator asked to stop at.
//
// Verified says the debugger accepted it. A breakpoint on a blank line or in
// code that was optimised away is REPORTED as unverified rather than dropped —
// silently discarding one leaves the operator waiting at a line that will never
// be reached.
type Breakpoint struct {
	File     string
	Line     int
	Verified bool
	// Message is why it was not verified, when the debugger said.
	Message string
	// Condition is an expression that must hold for the stop to happen. Empty
	// is an ordinary breakpoint.
	Condition string

	// Hits is how many times the line must be reached before it stops — the
	// adapter's own expression, because debuggers spell it differently ("5",
	// ">5", "%5"). Kept as the operator typed it rather than parsed here: a
	// number this package understood and the debugger did not would be a
	// breakpoint that stopped at the wrong time and said nothing.
	Hits string

	// Log is a message to print INSTEAD of stopping, which is what a logpoint
	// is. `{x}` in it is the adapter's own interpolation, so it is carried
	// through untouched.
	Log string
}

// Logpoint reports whether this breakpoint prints rather than stops.
func (b Breakpoint) Logpoint() bool { return b.Log != "" }

// Plain reports whether this is an ordinary breakpoint: stop here, every time.
func (b Breakpoint) Plain() bool {
	return b.Condition == "" && b.Hits == "" && b.Log == ""
}

// Frame is one entry on the call stack.
//
// File and Line may be empty: a frame inside the runtime, or inside a library
// built without source, has no place to jump to, and inventing one would send
// the reader somewhere arbitrary.
type Frame struct {
	ID    int
	Name  string
	File  string
	Line  int
	Scope int
}

// Variable is one name in scope.
//
// Children is how many the debugger says it has, not the children themselves: a
// slice of a million elements is asked for a page at a time, and a struct that
// expanded itself on arrival would fetch the whole heap to draw one row.
type Variable struct {
	Name     string
	Value    string
	Type     string
	Children int
	// Ref is the handle a caller asks for the children with. Zero means there
	// are none to ask for.
	Ref int
}

// Stop is everything known at the moment execution stopped.
type Stop struct {
	Thread int
	Reason Reason
	// Text is the adapter's own description, when it gave one — an exception's
	// message, most usefully.
	Text   string
	Frames []Frame
}

// Session is what a window shows about a run.
type Session struct {
	State       State
	Stop        Stop
	Breakpoints []Breakpoint
	// Err is why the session could not start or could not continue. It is not a
	// program that exited non-zero: a debuggee that failed is a debugging
	// session that worked.
	Err error
}

// Debugging reports whether there is a live session to act on, which is what
// every step and continue control is enabled by.
func (s Session) Debugging() bool { return s.State == Running || s.State == Stopped }

// AtRest reports whether the debuggee is stopped and can be inspected. Reading
// variables while it runs answers about a moment that has already passed.
func (s Session) AtRest() bool { return s.State == Stopped }
