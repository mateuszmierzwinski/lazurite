package debug

import "strings"

// The watch list: the expressions the operator wants answered at every stop.
//
// A LIST rather than a set of values, and the distinction is the whole design:
// an expression is kept, and its value is whatever it came to at the last stop.
// The other way round — keeping values and re-deriving the expressions — is how
// a watch list comes to show a number from three stops ago under a name that no
// longer means the same thing.

// MaxWatches bounds the list.
//
// Every one of them is a request to the debug adapter on every stop, and a stop
// that had to answer two hundred evaluations before the stack appeared would
// make stepping feel broken. Nobody reads more than a screenful anyway.
const MaxWatches = 32

// Watch is one expression and what it last came to.
type Watch struct {
	// Expression is what the operator typed, kept exactly: a debugger's
	// language is not this editor's to normalise.
	Expression string

	// Value is the last answer, as a variable so the pane can expand it
	// through the same path an ordinary variable takes.
	Value Variable

	// Err is what the debugger said instead, and is the ordinary case rather
	// than an exception: an expression is written before the frame that would
	// answer it exists, and `undefined: x` is the answer.
	Err string

	// Fresh says the value is from the CURRENT stop. A watch answered at the
	// last stop and not at this one is shown stale rather than removed — the
	// expression is still the operator's, and a row that disappeared when
	// stepping into another function would be a list that empties itself.
	Fresh bool
}

// Watches is the expressions being watched, in the order they were added.
//
// A plain value with no lock: it belongs to the window, is changed on the frame
// thread, and the evaluations that fill it arrive through the window's own post
// queue like everything else.
type Watches struct {
	list []Watch
}

// Add keeps an expression, answering whether it was added.
//
// An EMPTY expression is refused, and so is one already being watched: a list
// with the same expression twice answers the same question twice on every stop
// and gives the operator two rows to keep in step by hand.
func (w *Watches) Add(expression string) bool {
	expression = strings.TrimSpace(expression)
	if expression == "" || len(w.list) >= MaxWatches || w.Has(expression) {
		return false
	}
	w.list = append(w.list, Watch{Expression: expression})
	return true
}

// Has reports whether an expression is already watched.
func (w *Watches) Has(expression string) bool {
	return w.indexOf(strings.TrimSpace(expression)) >= 0
}

// Remove drops one expression, answering whether there was one.
func (w *Watches) Remove(expression string) bool {
	at := w.indexOf(strings.TrimSpace(expression))
	if at < 0 {
		return false
	}
	w.list = append(w.list[:at], w.list[at+1:]...)
	return true
}

// RemoveAt drops the watch at an index, which is what a selected row is.
func (w *Watches) RemoveAt(at int) bool {
	if at < 0 || at >= len(w.list) {
		return false
	}
	w.list = append(w.list[:at], w.list[at+1:]...)
	return true
}

// List is the watches, in order.
func (w *Watches) List() []Watch { return append([]Watch(nil), w.list...) }

// Len is how many there are.
func (w *Watches) Len() int { return len(w.list) }

// Expressions are what has to be asked at the next stop.
func (w *Watches) Expressions() []string {
	out := make([]string, 0, len(w.list))
	for _, watch := range w.list {
		out = append(out, watch.Expression)
	}
	return out
}

// Answer records what an expression came to.
//
// Keyed by the EXPRESSION rather than by position, because the answers arrive
// on their own goroutine and the operator may have added or removed a row in
// between: an answer written by index would land on somebody else's line.
func (w *Watches) Answer(expression string, value Variable, err string) {
	at := w.indexOf(strings.TrimSpace(expression))
	if at < 0 {
		return
	}
	w.list[at] = Watch{Expression: w.list[at].Expression, Value: value, Err: err, Fresh: true}
}

// Stale marks every watch as belonging to a previous stop, which is what
// resuming means.
//
// The values are KEPT rather than cleared: a list that emptied itself every
// time the operator pressed Continue would make the feature useless for exactly
// the thing it is for — watching one value change as the program runs.
func (w *Watches) Stale() {
	for i := range w.list {
		w.list[i].Fresh = false
	}
}

// indexOf is where an expression is, or -1.
func (w *Watches) indexOf(expression string) int {
	for i, watch := range w.list {
		if watch.Expression == expression {
			return i
		}
	}
	return -1
}
