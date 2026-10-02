package ui

import (
	"unicode/utf8"

	"github.com/mateuszmierzwinski/lazurite/internal/core/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/lexer"
)

// kindStyles is the colour of each kind of syntax.
var kindStyles = map[lexer.Kind]term.Style{
	lexer.Comment:     Comment,
	lexer.String:      Literal,
	lexer.Number:      Number,
	lexer.Keyword:     Keyword,
	lexer.Declaration: Declared,
}

// DrawEditor paints the part of a text that is in view, coloured, and puts
// the screen's cursor on the caret.
func DrawEditor(s *term.Screen, r term.Rect, ed *edit.Editor, p Picture) {
	if !p.Free {
		ed.Reveal(r.W, r.H)
	}
	for row := 0; row < r.H; row++ {
		line := ed.ViewTop + row
		if line >= ed.Buf.Lines() {
			break
		}
		drawLine(s, term.Rect{X: r.X, Y: r.Y + row, W: r.W, H: 1}, ed, p, line)
	}
	s.CurX, s.CurY = r.X+ed.Column()-ed.ViewLeft, r.Y+ed.Caret.Line-ed.ViewTop
	s.CursorOn = s.CurX >= r.X && s.CurX < r.X+r.W && s.CurY >= r.Y && s.CurY < r.Y+r.H
}

// drawLine paints one line of the text: its runes in their colours, tabs
// opened out, the line's own background when it carries a breakpoint or the
// debugger rests on it.
func drawLine(s *term.Screen, r term.Rect, ed *edit.Editor, p Picture, line int) {
	runes := ed.Buf.Line(line)
	styles := lineStyles(runes, spansOf(p, line))
	over, whole := lineBackground(p, line)
	if whole {
		s.Fill(r, ' ', over)
	}
	from, to := ed.Selection()
	col := 0
	for i, ch := range runes {
		st := cellStyle(styles[i], p, edit.Pos{Line: line, Col: i}, over, whole)
		if ed.Selected() && !(edit.Pos{Line: line, Col: i}).Before(from) && (edit.Pos{Line: line, Col: i}).Before(to) {
			st = Selection
		}
		col += putRune(s, r, col-ed.ViewLeft, ch, col, st)
	}
}

// putRune draws one rune at a visual column and returns how many columns
// it took; a tab is drawn as the blanks up to the next stop.
func putRune(s *term.Screen, r term.Rect, x int, ch rune, col int, st term.Style) int {
	w := 1
	if ch == '\t' {
		w, ch = edit.TabWidth-col%edit.TabWidth, ' '
	}
	for i := 0; i < w; i++ {
		if x+i >= 0 && x+i < r.W {
			s.Put(r.X+x+i, r.Y, ch, st)
		}
	}
	return w
}

// spansOf is one line's colouring; none for a line the picture has not
// coloured.
func spansOf(p Picture, line int) []lexer.Span {
	if line < 0 || line >= len(p.Spans) {
		return nil
	}
	return p.Spans[line]
}

// lineStyles is a style for each rune of a line, from the spans that colour
// it. Spans count bytes; the line is runes; the conversion is here.
func lineStyles(runes []rune, spans []lexer.Span) []term.Style {
	out := make([]term.Style, len(runes))
	at, span := 0, 0
	for i, r := range runes {
		out[i] = Text
		for span < len(spans) && spans[span].To <= at {
			span++
		}
		if span < len(spans) && spans[span].From <= at {
			if st, ok := kindStyles[spans[span].Kind]; ok {
				out[i] = st
			}
		}
		at += utf8.RuneLen(r)
	}
	return out
}

// lineBackground is the style a whole line takes when the debugger rests on
// it or it carries a breakpoint, and whether it takes one.
func lineBackground(p Picture, line int) (term.Style, bool) {
	switch {
	case p.Resume == line:
		return ResumeLine, true
	case p.Breaks[line]:
		return BreakLine, true
	}
	return term.Style{}, false
}

// cellStyle is the style of one rune: the line's own background when it has
// one, the mark of a fault when it is inside one, else its syntax colour.
func cellStyle(syntax term.Style, p Picture, at edit.Pos, over term.Style, whole bool) term.Style {
	if whole {
		return over
	}
	for _, m := range p.Marks {
		if !at.Before(m.From) && at.Before(m.To) {
			if m.Severe {
				return Fault
			}
			return Doubt
		}
	}
	return syntax
}

// ByLine sorts a scan's spans into a slice per line.
func ByLine(spans []lexer.Span, lines int) [][]lexer.Span {
	out := make([][]lexer.Span, lines)
	for _, sp := range spans {
		if sp.Line >= 0 && sp.Line < lines {
			out[sp.Line] = append(out[sp.Line], sp)
		}
	}
	return out
}

// PosAt is the position in a text under a cell of the rectangle it was
// drawn in.
func PosAt(ed *edit.Editor, r term.Rect, x, y int) edit.Pos {
	line := min(max(ed.ViewTop+y-r.Y, 0), ed.Buf.Lines()-1)
	return edit.Pos{Line: line, Col: edit.ColAt(ed.Buf.Line(line), ed.ViewLeft+x-r.X)}
}
