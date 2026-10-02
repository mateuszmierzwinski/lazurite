package delve

import (
	"context"
	"strconv"

	"github.com/go-delve/delve/pkg/proc"
	"github.com/go-delve/delve/service/api"
	"github.com/go-delve/delve/service/debugger"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
)

// Reading a stopped program: the stack, what is in scope, and one expression.
//
// The one thing that needs translating is the HANDLE. The window asks for a
// variable's children by a number it was given (`Variable.Ref`), which is DAP's
// model; Delve has no such thing and answers with whole values, loaded to a
// configured depth. So this file keeps the numbers: a ref stands for a
// goroutine, a frame and an expression, and expanding one asks for that
// expression again a level deeper.

// The load bounds. Each is what a PANE can draw rather than what a program may
// contain: a string field of a megabyte and a slice of a million elements are
// both ordinary in a running program and neither belongs in a table row.
var loadOnce = proc.LoadConfig{
	FollowPointers:     true,
	MaxVariableRecurse: 1,
	MaxStringLen:       512,
	MaxArrayValues:     64,
	MaxStructFields:    -1,
}

// maxFrames bounds a stack trace. A goroutine a thousand frames deep is a
// runaway recursion, and the operator reads the top of it either way.
const maxFrames = 128

// Frames is the call stack where execution is stopped.
func (c *Client) Frames(context.Context) ([]debug.Frame, error) {
	live, goroutine, err := c.resting()
	if err != nil {
		return nil, err
	}
	raw, err := live.Stacktrace(goroutine, maxFrames, 0)
	if err != nil {
		return nil, err
	}
	converted, err := live.ConvertStacktrace(raw, nil)
	if err != nil {
		return nil, err
	}
	return framesOf(converted), nil
}

// Variables is what is in scope, one level at a time.
//
// A zero ref is the selected frame's own scope: its arguments and its locals,
// in that order, because that is the order they are written in the source and
// the order a reader looks for them.
func (c *Client) Variables(_ context.Context, ref int) ([]debug.Variable, error) {
	live, goroutine, err := c.resting()
	if err != nil {
		return nil, err
	}
	if ref == 0 {
		return c.scopeOf(live, goroutine, 0)
	}
	held, known := c.held(ref)
	if !known {
		return nil, debug.ErrNotDebugging
	}
	return c.childrenOf(live, held)
}

// scopeOf is one frame's arguments and locals.
func (c *Client) scopeOf(live *debugger.Debugger, goroutine int64, frame int) ([]debug.Variable, error) {
	args, err := live.FunctionArguments(goroutine, frame, 0, loadOnce)
	if err != nil {
		return nil, err
	}
	locals, err := live.LocalVariables(goroutine, frame, 0, loadOnce)
	if err != nil {
		return nil, err
	}
	out := c.convertAll(goroutine, frame, args)
	return append(out, c.convertAll(goroutine, frame, locals)...), nil
}

// childrenOf expands one held expression a level deeper.
func (c *Client) childrenOf(live *debugger.Debugger, held reference) ([]debug.Variable, error) {
	value, err := live.EvalVariableInScope(held.goroutine, held.frame, 0, held.expression, loadOnce)
	if err != nil {
		return nil, err
	}
	converted := api.ConvertVar(value)
	out := make([]debug.Variable, 0, len(converted.Children))
	for i := range converted.Children {
		child := converted.Children[i]
		out = append(out, c.convert(held.goroutine, held.frame, held.expression, &child))
	}
	return out, nil
}

// Evaluate works out an expression in a frame.
//
// The FRAME is the window's own selection, and it is a stack index rather than
// Delve's frame identifier — the window numbers frames from the top, which is
// the order Frames answered in.
func (c *Client) Evaluate(_ context.Context, expression string, frame int) (debug.Variable, error) {
	live, goroutine, err := c.resting()
	if err != nil {
		return debug.Variable{}, err
	}
	value, err := live.EvalVariableInScope(goroutine, frame, 0, expression, loadOnce)
	if err != nil {
		return debug.Variable{}, err
	}
	converted := api.ConvertVar(value)
	converted.Name = expression
	return c.convert(goroutine, frame, "", converted), nil
}

// resting is the live debugger and the goroutine it is stopped in, or why not.
func (c *Client) resting() (*debugger.Debugger, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.live == nil:
		return nil, 0, debug.ErrNotDebugging
	case c.running:
		return nil, 0, debug.ErrRunning
	}
	return c.live, goroutineOf(c.state), nil
}

// goroutineOf is the goroutine the last stop selected, or -1 for whichever
// Delve considers current — which is what an inspection before any stop means.
func goroutineOf(state *api.DebuggerState) int64 {
	if state == nil || state.SelectedGoroutine == nil {
		return -1
	}
	return state.SelectedGoroutine.ID
}

// framesOf converts a stack trace.
//
// A frame with no source keeps an empty File rather than being dropped: a frame
// inside the runtime is part of the story of how execution arrived, and
// removing it makes the stack lie about its own shape.
func framesOf(frames []api.Stackframe) []debug.Frame {
	out := make([]debug.Frame, 0, len(frames))
	for i, frame := range frames {
		out = append(out, debug.Frame{
			ID: i, Name: frameName(frame), File: frame.File, Line: frame.Line,
		})
	}
	return out
}

// frameName is what the stack row says: the function, or the address where the
// binary carries no name for it.
func frameName(frame api.Stackframe) string {
	if frame.Function != nil && frame.Function.Name() != "" {
		return frame.Function.Name()
	}
	return "0x" + strconv.FormatUint(frame.PC, 16)
}
