package delve

import (
	"strconv"
	"strings"

	"github.com/go-delve/delve/pkg/proc"
	"github.com/go-delve/delve/service/api"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
)

// The handles a window expands a variable by.
//
// The window's model is DAP's: a variable carries a NUMBER, and asking for that
// number's children answers with the next level. Delve has no such handle — it
// evaluates expressions — so this file is the translation, and it is the whole
// of it: a number stands for a goroutine, a frame and an expression, and
// expanding it evaluates that expression again.
//
// The table is emptied on every stop. A handle from the last stop names a value
// that may not exist any more, and answering it would draw the previous
// moment's numbers under this moment's names.

// maxRefs bounds the table.
//
// Every expanded row costs one entry and the operator expands a handful. A
// table that grew for the life of a session would keep every value they ever
// looked at, in a process that is also running a debugger.
const maxRefs = 4096

// reference is what one handle stands for.
type reference struct {
	goroutine  int64
	frame      int
	expression string
}

// convertAll turns a scope's values into rows the window can draw.
func (c *Client) convertAll(goroutine int64, frame int, values []*proc.Variable) []debug.Variable {
	out := make([]debug.Variable, 0, len(values))
	for _, value := range values {
		out = append(out, c.convert(goroutine, frame, "", api.ConvertVar(value)))
	}
	return out
}

// convert turns one value into a row, keeping a handle where it has children.
//
// The PARENT's expression is carried in, because a child's own name is not an
// expression: `x` inside a struct is `parent.x`, and an editor that evaluated
// the bare name would answer about a different variable or about nothing.
func (c *Client) convert(goroutine int64, frame int, parent string, value *api.Variable) debug.Variable {
	out := debug.Variable{
		Name:  value.Name,
		Value: valueText(value),
		Type:  value.Type,
	}
	if children := childCount(value); children > 0 {
		out.Children = children
		out.Ref = c.keep(reference{
			goroutine: goroutine, frame: frame,
			expression: expressionFor(parent, value),
		})
	}
	return out
}

// valueText is what the value column says.
//
// Delve's own rendering, with a REASON where it could not read the value:
// "unreadable <nil>" is a fact about the program, and a blank cell reads as the
// debugger failing.
func valueText(value *api.Variable) string {
	if value.Unreadable != "" {
		return "unreadable: " + value.Unreadable
	}
	if value.Value != "" {
		return value.Value
	}
	return summaryOf(value)
}

// summaryOf is what to show for a value with no rendering of its own: a struct,
// a slice, a map. Its SHAPE, which is what a collapsed row is for.
func summaryOf(value *api.Variable) string {
	switch {
	case value.Len > 0:
		return value.Type + " (" + strconv.FormatInt(value.Len, 10) + ")"
	case len(value.Children) > 0:
		return value.Type
	default:
		return ""
	}
}

// childCount is how many children a value has, preferring what Delve COUNTED
// over what it loaded: a slice of a million elements is loaded a page at a time
// and the count is the honest number.
func childCount(value *api.Variable) int {
	if value.Len > 0 {
		return int(value.Len)
	}
	return len(value.Children)
}

// expressionFor is how a child is named to the debugger.
//
// An INDEXED child of a slice or an array is `parent[3]`, and a named one is
// `parent.field`. A top-level variable is its own name. Getting this wrong does
// not fail loudly: it evaluates something else that happens to exist.
func expressionFor(parent string, value *api.Variable) string {
	if parent == "" {
		return value.Name
	}
	if index, numbered := strconv.Atoi(value.Name); numbered == nil {
		return parent + "[" + strconv.Itoa(index) + "]"
	}
	if strings.HasPrefix(value.Name, "*") {
		return "(" + parent + ")" + value.Name
	}
	return parent + "." + value.Name
}

// keep records a handle and answers its number.
func (c *Client) keep(held reference) int {
	c.refMu.Lock()
	defer c.refMu.Unlock()
	if c.refs == nil {
		c.refs = map[int]reference{}
	}
	if len(c.refs) >= maxRefs {
		return 0
	}
	c.nextRef++
	c.refs[c.nextRef] = held
	return c.nextRef
}

// held is what a handle stands for, and whether it is still one.
func (c *Client) held(ref int) (reference, bool) {
	c.refMu.Lock()
	defer c.refMu.Unlock()
	found, known := c.refs[ref]
	return found, known
}

// forgetRefs empties the table, which is what a resumed program means: a handle
// from the last stop names a value that may not exist any more.
func (c *Client) forgetRefs() {
	c.refMu.Lock()
	defer c.refMu.Unlock()
	c.refs = map[int]reference{}
}
