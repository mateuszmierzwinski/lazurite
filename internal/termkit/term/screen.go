package term

import (
	"encoding/base64"
	"strconv"
	"unicode"
)

// NewScreen makes a blank screen of a size, held inside the largest drawn.
func NewScreen(w, h int) *Screen {
	w, h = min(max(w, 1), MaxWidth), min(max(h, 1), MaxHeight)
	s := &Screen{W: w, H: h, Cells: make([]Cell, w*h)}
	s.Fill(Rect{W: w, H: h}, ' ', Style{Fg: Grey, Bg: Black})
	return s
}

// Put draws one character; a cell outside the screen is ignored. A control
// character is drawn as a space: nothing a file holds may reach the
// terminal as a command.
func (s *Screen) Put(x, y int, r rune, st Style) {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return
	}
	s.Cells[y*s.W+x] = Cell{R: printable(r), S: st}
}

// The marks drawn in place of characters that must not be drawn as
// themselves.
const (
	// HiddenMark stands for a character that changes how the text around
	// it is ordered or joined without showing anything itself.
	HiddenMark = '¿'
	// InvalidMark stands for bytes that are not text.
	InvalidMark = '?'
)

// printable is a rune as it may be shown: itself; a space when it is a
// control character; a mark when it is not valid text, or when it is one
// of the invisible characters that reorder or join the text around them.
//
// Those last are shown because they can be used to make source code read
// differently from what the compiler sees ("Trojan Source"): an editor
// that draws them as nothing is helping.
func printable(r rune) rune {
	switch {
	case r == unicode.ReplacementChar:
		return InvalidMark
	case r < ' ' || r == 0x7f || (r >= 0x80 && r < 0xa0):
		return ' '
	case hidden(r):
		return HiddenMark
	}
	return r
}

// hidden reports whether a rune is an invisible character that reorders or
// joins text: the bidirectional controls, the zero-width characters, the
// byte-order mark.
func hidden(r rune) bool {
	switch {
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	case r >= 0x200b && r <= 0x200f, r == 0x2060, r == 0x061c, r == 0xfeff:
		return true
	}
	return false
}

// uneven reports whether a terminal draws a rune in other than one cell: a
// wide character takes two, a combining mark none. After one, the place of
// the next cell cannot be assumed and is set again.
func uneven(r rune) bool {
	if r < 0x300 {
		return false
	}
	return unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || wide(r)
}

// wide reports whether a rune is drawn two cells wide: the East Asian wide
// and fullwidth blocks, and the emoji.
func wide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3:
		return true
	case r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60, r >= 0xffe0 && r <= 0xffe6:
		return true
	case r >= 0x1f300 && r <= 0x1faff, r >= 0x20000 && r <= 0x3fffd:
		return true
	}
	return false
}

// Text draws a string from a cell rightwards, clipped to a width, and
// returns how many cells it used.
func (s *Screen) Text(x, y int, text string, st Style, width int) int {
	n := 0
	for _, r := range text {
		if n >= width {
			break
		}
		s.Put(x+n, y, r, st)
		n++
	}
	return n
}

// Fill paints a rectangle with one character.
func (s *Screen) Fill(r Rect, ch rune, st Style) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			s.Put(x, y, ch, st)
		}
	}
}

// Box draws a frame around a rectangle and clears its inside.
func (s *Screen) Box(r Rect, st Style) {
	if r.W < 2 || r.H < 2 {
		return
	}
	s.Fill(r, ' ', st)
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		s.Put(x, r.Y, '─', st)
		s.Put(x, r.Y+r.H-1, '─', st)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		s.Put(r.X, y, '│', st)
		s.Put(r.X+r.W-1, y, '│', st)
	}
	s.Put(r.X, r.Y, '┌', st)
	s.Put(r.X+r.W-1, r.Y, '┐', st)
	s.Put(r.X, r.Y+r.H-1, '└', st)
	s.Put(r.X+r.W-1, r.Y+r.H-1, '┘', st)
}

// At reads one cell; outside the screen it is the zero cell.
func (s *Screen) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return Cell{}
	}
	return s.Cells[y*s.W+x]
}

// Render is the bytes that turn a terminal showing prev into one showing
// next. With no prev, or one of another size, the whole screen is drawn.
// Only the cells that differ are sent: a keystroke redraws a few
// characters, not eight thousand.
func Render(prev, next *Screen) []byte {
	full := prev == nil || prev.W != next.W || prev.H != next.H
	out := []byte("\x1b[?25l")
	if full {
		out = append(out, "\x1b[0m\x1b[2J"...)
	}
	last, styled := Style{}, false
	for y := 0; y < next.H; y++ {
		out, last, styled = renderRow(out, prev, next, y, full, last, styled)
	}
	out = append(out, "\x1b[0m"...)
	return cursor(out, next)
}

// renderRow appends what changed on one row.
func renderRow(out []byte, prev, next *Screen, y int, full bool, last Style, styled bool) ([]byte, Style, bool) {
	at := -1
	for x := 0; x < next.W; x++ {
		c := next.Cells[y*next.W+x]
		if !full && prev.Cells[y*next.W+x] == c {
			continue
		}
		if at != x {
			out = moveTo(out, x, y)
		}
		if !styled || c.S != last {
			out, last, styled = append(out, sgr(c.S)...), c.S, true
		}
		out = append(out, string(c.R)...)
		at = x + 1
		if uneven(c.R) {
			at = -1
		}
	}
	return out, last, styled
}

// moveTo is the sequence that puts the terminal's cursor on a cell.
func moveTo(out []byte, x, y int) []byte {
	out = append(out, "\x1b["...)
	out = strconv.AppendInt(out, int64(y+1), 10)
	out = append(out, ';')
	out = strconv.AppendInt(out, int64(x+1), 10)
	return append(out, 'H')
}

// cursor ends a frame by showing the cursor where the screen wants it, or
// leaving it hidden.
func cursor(out []byte, s *Screen) []byte {
	if !s.CursorOn || s.CurX < 0 || s.CurY < 0 || s.CurX >= s.W || s.CurY >= s.H {
		return out
	}
	out = moveTo(out, s.CurX, s.CurY)
	return append(out, "\x1b[?25h"...)
}

// sgr is the sequence that selects a style.
func sgr(st Style) string {
	out := "\x1b[0"
	if st.Underline {
		out += ";4"
	}
	return out + ";" + strconv.Itoa(fgCode(st.Fg)) + ";" + strconv.Itoa(fgCode(st.Bg)+10) + "m"
}

// fgCode is a colour's ANSI foreground number: 30–37 for the dark eight,
// 90–97 for the bright.
func fgCode(c Color) int {
	if c < 8 {
		return 30 + int(c)
	}
	return 90 + int(c&7)
}

// The sequences that take a terminal over and give it back.
const (
	// Enter switches to the alternate screen, turns on mouse reporting
	// (press, drag, wheel; SGR coordinates) and bracketed paste.
	Enter = "\x1b[?1049h\x1b[?1000h\x1b[?1002h\x1b[?1006h\x1b[?2004h\x1b[2J"
	// Leave undoes Enter and shows the cursor.
	Leave = "\x1b[?2004l\x1b[?1006l\x1b[?1002l\x1b[?1000l\x1b[0m\x1b[?25h\x1b[?1049l"
)

// Clone is a copy of the screen that later drawing does not change: what a
// frame is compared against.
func (s *Screen) Clone() *Screen {
	c := *s
	c.Cells = append([]Cell(nil), s.Cells...)
	return &c
}

// maxClipboard bounds what is offered to the terminal's clipboard; a
// terminal drops a sequence it finds too long, and some drop the line with
// it.
const maxClipboard = 64 << 10

// Clipboard is the sequence that asks the terminal to put a text on the
// system clipboard (OSC 52). A terminal that does not know it shows
// nothing; a text too long to offer is not offered.
func Clipboard(text string) []byte {
	if len(text) == 0 || len(text) > maxClipboard {
		return nil
	}
	return []byte("\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07")
}
