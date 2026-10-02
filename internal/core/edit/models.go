// Package edit is the text being edited: a buffer of lines that remembers
// how to undo itself, and an editor over it — a caret, a selection, and
// what each key does to them.
//
// It is pure: it knows nothing of screens, files or languages. Positions
// count RUNES within a line; a consumer that needs another counting (a
// language server's UTF-16, a screen's columns) converts.
package edit

// Pos is a place in a buffer: a line and a rune offset within it, both from
// zero. Col may equal the line's length: the place after its last rune.
type Pos struct {
	Line, Col int
}

// Before reports whether p comes before q.
func (p Pos) Before(q Pos) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Col < q.Col)
}

// change is one edit, as undo needs it: where it happened, what was there,
// what replaced it.
type change struct {
	at       Pos
	removed  string
	inserted string
	// typed marks a single typed character, which may join the change
	// before it into one undo step.
	typed bool
}

// Buffer is text as lines.
type Buffer struct {
	lines [][]rune
	undo  []change
	redo  []change
	// sealed stops the next typed character joining the last change.
	sealed bool
	// rev counts the edits made, undone and redone: two readings that
	// differ mean the text may have changed in between.
	rev int
}

// The limits of a buffer.
const (
	// MaxUndo is how many steps back a buffer remembers.
	MaxUndo = 2000
	// TabWidth is how many columns a tab stop spans.
	TabWidth = 4
)

// Editor is a buffer being edited: where the caret is, what is selected,
// and what part of the text is in view.
type Editor struct {
	Buf *Buffer
	// Caret is where typing happens. Anchor is where the selection began;
	// with no selection it equals Caret.
	Caret, Anchor Pos
	// want is the visual column the caret keeps to while moving up and down
	// through shorter lines.
	want int
	// ViewTop and ViewLeft are the first line and visual column in view.
	ViewTop, ViewLeft int
}
