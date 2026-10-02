package ui

import (
	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
)

// NewInput makes an input line holding a text, the caret at its end.
func NewInput(text string) Input {
	runes := []rune(text)
	return Input{Text: runes, Caret: len(runes)}
}

// String is what has been typed.
func (i *Input) String() string { return string(i.Text) }

// Key gives the line a key and says whether it changed the text.

// remove deletes the rune at an index, when there is one.
func (i *Input) remove(at int) bool {
	if at < 0 || at >= len(i.Text) {
		return false
	}
	i.Text = append(i.Text[:at], i.Text[at+1:]...)
	i.Caret = at
	return true
}

// typed inserts a typed character; a key held with Ctrl or Alt is not one.
func (i *Input) typed(ev term.Event) bool {
	if ev.Mods&(term.Ctrl|term.Alt) != 0 || len(i.Text) >= MaxInput {
		return false
	}
	i.Text = append(i.Text[:i.Caret], append([]rune{ev.Rune}, i.Text[i.Caret:]...)...)
	i.Caret++
	return true
}

// Paste inserts a pasted text at the caret, on one line.
func (i *Input) Paste(text string) {
	for _, r := range text {
		if r == '\n' || r == '\r' {
			break
		}
		i.typed(term.Event{Rune: r})
	}
}

// Draw paints the line in a width and puts the screen's cursor on its
// caret.
func (i *Input) Draw(s *term.Screen, x, y, w int) {
	s.Fill(term.Rect{X: x, Y: y, W: w, H: 1}, ' ', Field)
	from := max(i.Caret-w+1, 0)
	s.Text(x, y, string(i.Text[from:]), Field, w)
	s.CurX, s.CurY, s.CursorOn = x+i.Caret-from, y, true
}

// Key gives the list a key and says whether it was one of the list's.

// Move moves the choice by some rows, staying inside the list.
func (l *List) Move(by int) {
	l.Sel = min(max(l.Sel+by, 0), max(len(l.Items)-1, 0))
}

// Chosen is the chosen line; false for an empty list.
func (l *List) Chosen() (string, bool) {
	if l.Sel < 0 || l.Sel >= len(l.Items) {
		return "", false
	}
	return l.Items[l.Sel], true
}

// Set replaces the lines, keeping the choice inside them.
func (l *List) Set(items []string) {
	l.Items = items
	l.Move(0)
}

// Draw paints the list in a rectangle, scrolled so the chosen line shows.
// With no focus the chosen line is drawn like the others.
func (l *List) Draw(s *term.Screen, r term.Rect, plain, chosen term.Style, focus bool) {
	if r.H < 1 {
		return
	}
	l.Top = min(max(l.Top, l.Sel-r.H+1), l.Sel)
	l.Top = max(min(l.Top, len(l.Items)-r.H), 0)
	for row := 0; row < r.H; row++ {
		i := l.Top + row
		st := plain
		if focus && i == l.Sel && i < len(l.Items) {
			st = chosen
		}
		s.Fill(term.Rect{X: r.X, Y: r.Y + row, W: r.W, H: 1}, ' ', st)
		if i < len(l.Items) {
			s.Text(r.X+1, r.Y+row, l.Items[i], st, r.W-1)
		}
	}
}

// Click chooses the line at a row of the rectangle the list was drawn in,
// and says whether there is one.
func (l *List) Click(row int) bool {
	i := l.Top + row
	if row < 0 || i >= len(l.Items) {
		return false
	}
	l.Sel = i
	return true
}
