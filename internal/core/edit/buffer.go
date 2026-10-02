package edit

import "strings"

// NewBuffer makes a buffer holding a text. Line endings of other systems
// are read as plain newlines.
func NewBuffer(text string) *Buffer {
	return &Buffer{lines: split(text), sealed: true}
}

// split is a text as lines of runes; a text always has at least one line.
func split(text string) [][]rune {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	parts := strings.Split(text, "\n")
	out := make([][]rune, len(parts))
	for i, p := range parts {
		out[i] = []rune(p)
	}
	return out
}

// Text is the whole buffer, lines joined by newlines.
func (b *Buffer) Text() string {
	var sb strings.Builder
	for i, line := range b.lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(line))
	}
	return sb.String()
}

// Rev is a number that changes whenever the text does. A consumer that
// derived something from the text — its colours, what was saved — keeps the
// number it derived it at and knows it is stale when the two differ.
func (b *Buffer) Rev() int { return b.rev }

// Lines is how many lines the buffer has; never less than one.
func (b *Buffer) Lines() int { return len(b.lines) }

// Line is one line's runes; a line that does not exist is empty. The slice
// is the buffer's own: read it, do not keep or change it.
func (b *Buffer) Line(i int) []rune {
	if i < 0 || i >= len(b.lines) {
		return nil
	}
	return b.lines[i]
}

// Clamp is a position moved inside the buffer.
func (b *Buffer) Clamp(p Pos) Pos {
	p.Line = min(max(p.Line, 0), len(b.lines)-1)
	p.Col = min(max(p.Col, 0), len(b.lines[p.Line]))
	return p
}

// End is the position after the buffer's last rune.
func (b *Buffer) End() Pos {
	last := len(b.lines) - 1
	return Pos{Line: last, Col: len(b.lines[last])}
}

// Range is the text between two positions, in either order.
func (b *Buffer) Range(from, to Pos) string {
	from, to = b.ordered(from, to)
	if from.Line == to.Line {
		return string(b.lines[from.Line][from.Col:to.Col])
	}
	var sb strings.Builder
	sb.WriteString(string(b.lines[from.Line][from.Col:]))
	for l := from.Line + 1; l < to.Line; l++ {
		sb.WriteByte('\n')
		sb.WriteString(string(b.lines[l]))
	}
	sb.WriteByte('\n')
	sb.WriteString(string(b.lines[to.Line][:to.Col]))
	return sb.String()
}

// ordered is two positions clamped and put first-to-last.
func (b *Buffer) ordered(from, to Pos) (Pos, Pos) {
	from, to = b.Clamp(from), b.Clamp(to)
	if to.Before(from) {
		return to, from
	}
	return from, to
}

// Replace puts a text in place of what is between two positions and
// returns the position after it. It is one step of undo.
func (b *Buffer) Replace(from, to Pos, text string) Pos {
	from, to = b.ordered(from, to)
	removed := b.Range(from, to)
	if removed == "" && text == "" {
		return from
	}
	b.record(change{at: from, removed: removed, inserted: text})
	return b.splice(from, to, text)
}

// Type inserts one typed character. Characters typed one after another
// join into a single step of undo, until the buffer is sealed.
func (b *Buffer) Type(at Pos, r rune) Pos {
	at = b.Clamp(at)
	b.record(change{at: at, inserted: string(r), typed: true})
	return b.splice(at, at, string(r))
}

// Seal ends the run of typed characters that undo treats as one step: the
// caret moved, or something else happened.
func (b *Buffer) Seal() { b.sealed = true }

// record remembers a change for undo, joining a typed character onto the
// run before it, and forgets what could have been redone.
func (b *Buffer) record(c change) {
	b.redo = b.redo[:0]
	if n := len(b.undo); c.typed && !b.sealed && n > 0 && b.undo[n-1].typed && joins(b.undo[n-1], c) {
		b.undo[n-1].inserted += c.inserted
		return
	}
	b.sealed = false
	b.undo = append(b.undo, c)
	if len(b.undo) > MaxUndo {
		b.undo = b.undo[len(b.undo)-MaxUndo:]
	}
}

// joins reports whether a typed character continues the run before it:
// typed directly after it, on the same line, and not a newline.
func joins(prev, next change) bool {
	if strings.Contains(prev.inserted, "\n") || next.inserted == "\n" {
		return false
	}
	return prev.at.Line == next.at.Line && prev.at.Col+len([]rune(prev.inserted)) == next.at.Col
}

// splice is the edit itself, with nothing remembered: the runes between two
// positions give way to a text. It returns the position after the text.
func (b *Buffer) splice(from, to Pos, text string) Pos {
	b.rev++
	head := append([]rune{}, b.lines[from.Line][:from.Col]...)
	tail := append([]rune{}, b.lines[to.Line][to.Col:]...)
	mid := split(text)
	last := len(mid) - 1
	end := Pos{Line: from.Line + last, Col: len(mid[last])}
	if last == 0 {
		end.Col += len(head)
	}
	mid[0] = append(head, mid[0]...)
	mid[last] = append(mid[last], tail...)
	rest := append([][]rune{}, b.lines[to.Line+1:]...)
	b.lines = append(append(b.lines[:from.Line], mid...), rest...)
	return end
}

// endOf is the position after a text inserted at a place.
func endOf(at Pos, text string) Pos {
	lines := split(text)
	last := len(lines) - 1
	if last == 0 {
		return Pos{Line: at.Line, Col: at.Col + len(lines[0])}
	}
	return Pos{Line: at.Line + last, Col: len(lines[last])}
}

// Undo takes back the last step and returns where the caret belongs; false
// when there is nothing to take back.
func (b *Buffer) Undo() (Pos, bool) {
	n := len(b.undo)
	if n == 0 {
		return Pos{}, false
	}
	c := b.undo[n-1]
	b.undo = b.undo[:n-1]
	b.redo = append(b.redo, c)
	b.sealed = true
	return b.splice(c.at, endOf(c.at, c.inserted), c.removed), true
}

// Redo does again the last step undone and returns where the caret belongs.
func (b *Buffer) Redo() (Pos, bool) {
	n := len(b.redo)
	if n == 0 {
		return Pos{}, false
	}
	c := b.redo[n-1]
	b.redo = b.redo[:n-1]
	b.undo = append(b.undo, c)
	b.sealed = true
	return b.splice(c.at, endOf(c.at, c.removed), c.inserted), true
}

// SetText replaces everything, as one step of undo. A text equal to the
// buffer's own changes nothing and records nothing.
func (b *Buffer) SetText(text string) {
	if text == b.Text() {
		return
	}
	b.sealed = true
	b.Replace(Pos{}, b.End(), text)
	b.sealed = true
}
