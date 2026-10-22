package ui

import (
	"strconv"
	"strings"

	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

// The parts of the desktop that are not a widget of their own: the
// backdrop, window frames, shadows, the status line, and text with a
// highlighted letter.

// Backdrop paints the empty desktop.
func Backdrop(s *term.Screen, r term.Rect) {
	s.Fill(r, '░', Desktop)
}

// Hot draws a label whose picking letter is between tildes, that letter in
// its own style, and returns how many cells it used.
func Hot(s *term.Screen, x, y int, label string, plain, hot term.Style) int {
	n, on := 0, false
	for _, r := range label {
		if r == '~' {
			on = !on
			continue
		}
		st := plain
		if on {
			st = hot
		}
		s.Put(x+n, y, r, st)
		n++
	}
	return n
}

// HotLen is how many cells a label takes, its tildes not counted.
func HotLen(label string) int {
	return len([]rune(label)) - strings.Count(label, "~")
}

// HotKey is the letter that picks a label, in lower case; zero when it has
// none.
func HotKey(label string) rune {
	at := strings.IndexByte(label, '~')
	if at < 0 || at+1 >= len(label) {
		return 0
	}
	for _, r := range strings.ToLower(label[at+1:]) {
		return r
	}
	return 0
}

// Window draws a double-line frame with its title, and clears its inside.
func Window(s *term.Screen, r term.Rect, t Title, inside term.Style) {
	if r.W < 4 || r.H < 2 {
		return
	}
	edge := FrameIdle
	if t.Active {
		edge = Frame
	}
	edge.Bg = inside.Bg
	s.Fill(r, ' ', inside)
	doubleFrame(s, r, edge)
	titleBar(s, r, t, edge)
	if t.Foot != "" {
		s.Text(r.X+2, r.Y+r.H-1, " "+t.Foot+" ", edge, max(r.W-4, 0))
	}
}

// doubleFrame draws the four edges and corners of a window.
func doubleFrame(s *term.Screen, r term.Rect, st term.Style) {
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		s.Put(x, r.Y, '═', st)
		s.Put(x, r.Y+r.H-1, '═', st)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		s.Put(r.X, y, '║', st)
		s.Put(r.X+r.W-1, y, '║', st)
	}
	s.Put(r.X, r.Y, '╔', st)
	s.Put(r.X+r.W-1, r.Y, '╗', st)
	s.Put(r.X, r.Y+r.H-1, '╚', st)
	s.Put(r.X+r.W-1, r.Y+r.H-1, '╝', st)
}

// titleBar writes a window's top edge: the close mark, the name in the
// middle, the window's number at the right.
func titleBar(s *term.Screen, r term.Rect, t Title, edge term.Style) {
	mark := edge
	mark.Fg = FrameMark.Fg
	if t.Active && r.W > 12 {
		s.Text(r.X+2, r.Y, "[", edge, 1)
		s.Text(r.X+3, r.Y, "■", mark, 1)
		s.Text(r.X+4, r.Y, "]", edge, 1)
	}
	name := " " + t.Name + " "
	if t.Changed {
		name = " " + t.Name + " * "
	}
	room := max(r.W-titleMargin(t), 0)
	name = Clip(name, room)
	s.Text(r.X+(r.W-len([]rune(name)))/2, r.Y, name, edge, room)
	if t.Number > 0 && r.W > 12 {
		s.Text(r.X+r.W-5, r.Y, strconv.Itoa(t.Number), edge, 3)
	}
}

// titleMargin is how much of a window's top edge its title may not use:
// the corners and the close mark, and for a numbered window the number.
func titleMargin(t Title) int {
	if t.Number > 0 {
		return 16
	}
	return 10
}

// Clip is a text cut to a width, its head replaced by an ellipsis when it
// is too long — the end of a path is the part that tells files apart.
func Clip(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width < 2 {
		return string(runes[:max(width, 0)])
	}
	return "…" + string(runes[len(runes)-width+1:])
}

// Shadow darkens the cells a box casts its shadow on: two columns to its
// right and one row below, as the classic text desktop draws it.
func Shadow(s *term.Screen, r term.Rect) {
	for y := r.Y + 1; y <= r.Y+r.H; y++ {
		shade(s, r.X+r.W, y)
		shade(s, r.X+r.W+1, y)
	}
	for x := r.X + 2; x < r.X+r.W+2; x++ {
		shade(s, x, r.Y+r.H)
	}
}

// shade darkens one cell, keeping its character.
func shade(s *term.Screen, x, y int) {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return
	}
	s.Put(x, y, s.At(x, y).R, Shade)
}

// Status draws the bottom line: at the right a message, and at the left as
// many of the keys worth knowing as fit beside it. The message comes
// first: it is news, and the keys are always in the menus.
func Status(s *term.Screen, hints []Hint, message string) {
	y := s.H - 1
	s.Fill(term.Rect{Y: y, W: s.W, H: 1}, ' ', Bar)
	message = headClip(message, max(s.W-2, 0))
	room := s.W
	if message != "" {
		room = s.W - len([]rune(message)) - 3
		s.Text(s.W-len([]rune(message))-1, y, message, Bar, s.W)
	}
	x := 1
	for _, h := range hints {
		if x+len([]rune(h.Key))+len([]rune(h.Label))+1 > room {
			break
		}
		x += s.Text(x, y, h.Key, BarHot, s.W)
		x += s.Text(x, y, " "+h.Label, Bar, s.W) + 2
	}
}

// headClip is a text cut to a width keeping its beginning.
func headClip(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	return string(runes[:max(width-1, 0)]) + "…"
}

// Centered is a rectangle of a size in the middle of the screen, made to
// fit it.
func Centered(s *term.Screen, w, h int) term.Rect {
	w, h = min(w, max(s.W-4, 1)), min(h, max(s.H-2, 1))
	return term.Rect{X: (s.W - w) / 2, Y: (s.H - h) / 2, W: w, H: h}
}
