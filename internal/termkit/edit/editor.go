package edit

import (
	"strings"
	"unicode"
)

// NewEditor makes an editor over a text, the caret at its start.
func NewEditor(text string) *Editor {
	return &Editor{Buf: NewBuffer(text)}
}

// Selected reports whether anything is selected.
func (e *Editor) Selected() bool { return e.Anchor != e.Caret }

// Selection is the selected span, first position first.
func (e *Editor) Selection() (Pos, Pos) {
	if e.Caret.Before(e.Anchor) {
		return e.Caret, e.Anchor
	}
	return e.Anchor, e.Caret
}

// SelectedText is what is selected; empty with no selection.
func (e *Editor) SelectedText() string {
	return e.Buf.Range(e.Anchor, e.Caret)
}

// place moves the caret. With keep the selection grows from its anchor;
// without, the selection is dropped. The run of typing undo joins is ended.
func (e *Editor) place(p Pos, keep bool) {
	e.Caret = e.Buf.Clamp(p)
	if !keep {
		e.Anchor = e.Caret
	}
	e.Buf.Seal()
}

// Goto puts the caret at a position, dropping the selection.
func (e *Editor) Goto(p Pos) {
	e.place(p, false)
	e.want = e.Column()
}

// Select puts the selection between two positions, the caret on the second.
func (e *Editor) Select(from, to Pos) {
	e.Anchor = e.Buf.Clamp(from)
	e.Caret = e.Buf.Clamp(to)
	e.want = e.Column()
	e.Buf.Seal()
}

// SelectAll selects the whole text.
func (e *Editor) SelectAll() { e.Select(Pos{}, e.Buf.End()) }

// Column is the caret's visual column: where it is drawn, counting a tab as
// reaching the next tab stop.
func (e *Editor) Column() int {
	return Visual(e.Buf.Line(e.Caret.Line), e.Caret.Col)
}

// Visual is the visual column of a rune offset in a line.
func Visual(line []rune, col int) int {
	v := 0
	for i := 0; i < col && i < len(line); i++ {
		v += width(line[i], v)
	}
	return v
}

// width is how many columns a rune takes when drawn at a visual column.
func width(r rune, at int) int {
	if r == '\t' {
		return TabWidth - at%TabWidth
	}
	return 1
}

// ColAt is the rune offset in a line nearest a visual column.
func ColAt(line []rune, visual int) int {
	v := 0
	for i, r := range line {
		w := width(r, v)
		if v+w > visual {
			if visual-v >= w-(visual-v) && w > 1 {
				return i + 1
			}
			return i
		}
		v += w
	}
	return len(line)
}

// Left moves the caret one rune back, to the end of the line above at the
// start of a line.
func (e *Editor) Left(keep bool) {
	p := e.Caret
	switch {
	case e.Selected() && !keep:
		p, _ = e.Selection()
	case p.Col > 0:
		p.Col--
	case p.Line > 0:
		p.Line--
		p.Col = len(e.Buf.Line(p.Line))
	}
	e.place(p, keep)
	e.want = e.Column()
}

// Right moves the caret one rune on, to the start of the line below at the
// end of a line.
func (e *Editor) Right(keep bool) {
	p := e.Caret
	switch {
	case e.Selected() && !keep:
		_, p = e.Selection()
	case p.Col < len(e.Buf.Line(p.Line)):
		p.Col++
	case p.Line < e.Buf.Lines()-1:
		p.Line, p.Col = p.Line+1, 0
	}
	e.place(p, keep)
	e.want = e.Column()
}

// Vertical moves the caret some lines up (negative) or down, keeping to the
// visual column it started from.
func (e *Editor) Vertical(lines int, keep bool) {
	line := min(max(e.Caret.Line+lines, 0), e.Buf.Lines()-1)
	e.place(Pos{Line: line, Col: ColAt(e.Buf.Line(line), e.want)}, keep)
}

// Home moves to the first non-blank of the line, or to its very start when
// already there.
func (e *Editor) Home(keep bool) {
	line := e.Buf.Line(e.Caret.Line)
	first := 0
	for first < len(line) && (line[first] == ' ' || line[first] == '\t') {
		first++
	}
	if e.Caret.Col == first {
		first = 0
	}
	e.place(Pos{Line: e.Caret.Line, Col: first}, keep)
	e.want = e.Column()
}

// End moves to the end of the line.
func (e *Editor) End(keep bool) {
	e.place(Pos{Line: e.Caret.Line, Col: len(e.Buf.Line(e.Caret.Line))}, keep)
	e.want = e.Column()
}

// DocStart moves to the start of the text.
func (e *Editor) DocStart(keep bool) {
	e.place(Pos{}, keep)
	e.want = 0
}

// DocEnd moves to the end of the text.
func (e *Editor) DocEnd(keep bool) {
	e.place(e.Buf.End(), keep)
	e.want = e.Column()
}

// isWord reports whether a rune is part of a word.
func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// WordLeft moves to the start of the word before the caret.
func (e *Editor) WordLeft(keep bool) {
	p := e.Caret
	line := e.Buf.Line(p.Line)
	if p.Col == 0 {
		e.Left(keep)
		return
	}
	for p.Col > 0 && !isWord(line[p.Col-1]) {
		p.Col--
	}
	for p.Col > 0 && isWord(line[p.Col-1]) {
		p.Col--
	}
	e.place(p, keep)
	e.want = e.Column()
}

// WordRight moves to the start of the word after the caret.
func (e *Editor) WordRight(keep bool) {
	p := e.Caret
	line := e.Buf.Line(p.Line)
	if p.Col >= len(line) {
		e.Right(keep)
		return
	}
	for p.Col < len(line) && isWord(line[p.Col]) {
		p.Col++
	}
	for p.Col < len(line) && !isWord(line[p.Col]) {
		p.Col++
	}
	e.place(p, keep)
	e.want = e.Column()
}

// put replaces the selection (or nothing) with a text and leaves the caret
// after it.
func (e *Editor) put(text string) {
	from, to := e.Selection()
	e.Caret = e.Buf.Replace(from, to, text)
	e.Anchor = e.Caret
	e.want = e.Column()
}

// Insert puts a text at the caret, replacing the selection: a paste.
func (e *Editor) Insert(text string) {
	e.Buf.Seal()
	e.put(text)
	e.Buf.Seal()
}

// Type inserts one typed character, replacing the selection.
func (e *Editor) Type(r rune) {
	if e.Selected() {
		e.put(string(r))
		return
	}
	e.Caret = e.Buf.Type(e.Caret, r)
	e.Anchor = e.Caret
	e.want = e.Column()
}

// Enter breaks the line at the caret and indents the new one like the old:
// the same leading blanks, and one tab more after an opening brace.
func (e *Editor) Enter() {
	line := e.Buf.Line(e.Caret.Line)
	indent := leading(line)
	before := strings.TrimRight(string(line[:min(e.Caret.Col, len(line))]), " \t")
	if strings.HasSuffix(before, "{") || strings.HasSuffix(before, "(") || strings.HasSuffix(before, ":") {
		indent += "\t"
	}
	e.Buf.Seal()
	e.put("\n" + indent)
	e.Buf.Seal()
}

// leading is the blanks a line starts with.
func leading(line []rune) string {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return string(line[:n])
}

// Backspace deletes the selection, or the rune before the caret.
func (e *Editor) Backspace() {
	if !e.Selected() {
		e.Buf.Seal()
		e.Left(true)
	}
	e.put("")
	e.Buf.Seal()
}

// Delete deletes the selection, or the rune under the caret.
func (e *Editor) Delete() {
	if !e.Selected() {
		e.Buf.Seal()
		e.Right(true)
	}
	e.put("")
	e.Buf.Seal()
}

// Cut removes the selection and returns it.
func (e *Editor) Cut() string {
	text := e.SelectedText()
	if text != "" {
		e.Buf.Seal()
		e.put("")
	}
	return text
}

// Undo takes back the last step.
func (e *Editor) Undo() {
	if p, ok := e.Buf.Undo(); ok {
		e.Goto(p)
	}
}

// Redo does again the last step undone.
func (e *Editor) Redo() {
	if p, ok := e.Buf.Redo(); ok {
		e.Goto(p)
	}
}

// Reveal scrolls so that the caret is inside a view of a size.
func (e *Editor) Reveal(w, h int) {
	if w < 1 || h < 1 {
		return
	}
	if e.Caret.Line < e.ViewTop {
		e.ViewTop = e.Caret.Line
	}
	if e.Caret.Line >= e.ViewTop+h {
		e.ViewTop = e.Caret.Line - h + 1
	}
	col := e.Column()
	if col < e.ViewLeft {
		e.ViewLeft = col
	}
	if col >= e.ViewLeft+w {
		e.ViewLeft = col - w + 1
	}
}

// Scroll moves the view some lines without moving the caret.
func (e *Editor) Scroll(lines int) {
	e.ViewTop = min(max(e.ViewTop+lines, 0), max(e.Buf.Lines()-1, 0))
}

// Find selects the next occurrence of a text after the caret, wrapping
// round to the start. It reports whether there is one.
func (e *Editor) Find(needle string) bool {
	if needle == "" {
		return false
	}
	_, from := e.Selection()
	text := []rune(e.Buf.Text())
	start := e.offset(from)
	at := indexFrom(text, []rune(needle), start)
	if at < 0 {
		at = indexFrom(text, []rune(needle), 0)
	}
	if at < 0 {
		return false
	}
	e.Select(e.posOf(text, at), e.posOf(text, at+len([]rune(needle))))
	return true
}

// offset is a position as a rune offset into the whole text.
func (e *Editor) offset(p Pos) int {
	n := 0
	for l := 0; l < p.Line; l++ {
		n += len(e.Buf.Line(l)) + 1
	}
	return n + p.Col
}

// posOf is a rune offset into the whole text as a position.
func (e *Editor) posOf(text []rune, at int) Pos {
	p := Pos{}
	for i := 0; i < at && i < len(text); i++ {
		if text[i] == '\n' {
			p.Line, p.Col = p.Line+1, 0
			continue
		}
		p.Col++
	}
	return p
}

// indexFrom is where a needle first occurs in a text at or after an offset;
// -1 when it does not.
func indexFrom(text, needle []rune, from int) int {
	for i := max(from, 0); i+len(needle) <= len(text); i++ {
		if string(text[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}

// Word is the word the caret is in or just after, and where it starts.
func (e *Editor) Word() (string, Pos) {
	line := e.Buf.Line(e.Caret.Line)
	start := min(e.Caret.Col, len(line))
	for start > 0 && isWord(line[start-1]) {
		start--
	}
	return string(line[start:min(e.Caret.Col, len(line))]), Pos{Line: e.Caret.Line, Col: start}
}
