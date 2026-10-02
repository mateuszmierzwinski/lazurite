package ui

import (
	"strings"
	"unicode"

	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
)

// Message is a box of text with one way out.
type Message struct {
	Title string
	Lines []string
	list  List
}

// NewMessage makes a message box. A text longer than the box scrolls.
func NewMessage(title string, lines []string) *Message {
	return &Message{Title: title, Lines: lines, list: List{Items: lines}}
}

// Draw paints the box in the middle of the screen.
func (m *Message) Draw(s *term.Screen) {
	w := len([]rune(m.Title)) + titleRoom
	for _, line := range m.Lines {
		w = max(w, len([]rune(line))+6)
	}
	r := panel(s, m.Title, w, len(m.Lines)+5)
	rows := term.Rect{X: r.X + 2, Y: r.Y + 2, W: r.W - 4, H: r.H - 5}
	m.list.Draw(s, rows, Panel, Panel, false)
	button(s, r.X+(r.W-8)/2, r.Y+r.H-2, "OK", true)
}

// titleRoom is how much wider than its title a box must be for the title
// to fit between the marks a frame carries at its corners.
const titleRoom = 20

// Key closes the box on Enter or Escape and scrolls it on the arrows.
func (m *Message) Key(ev term.Event) Outcome {
	switch ev.Key {
	case term.KeyEnter, term.KeyEsc:
		return Accepted
	case term.KeyRune:
		if ev.Rune == ' ' {
			return Accepted
		}
	}
	m.list.Key(ev, 5)
	return Pending
}

// panel draws a dialog's box, shadow and title, and answers where it is.
func panel(s *term.Screen, title string, w, h int) term.Rect {
	r := Centered(s, w, h)
	Shadow(s, r)
	Window(s, r, Title{Name: title, Active: true}, PanelFrame)
	s.Fill(term.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, ' ', Panel)
	s.CursorOn = false
	return r
}

// button draws one button and returns its width.
func button(s *term.Screen, x, y int, label string, chosen bool) int {
	st := Button
	if chosen {
		st = ButtonChosen
	}
	text := "  " + label + "  "
	if chosen {
		text = "► " + label + " ◄"
	}
	return s.Text(x, y, text, st, s.W)
}

// Confirm asks a question with three answers: yes, no, and never mind.
type Confirm struct {
	Title, Text string
	// at is the button that has the keyboard: 0 yes, 1 no, 2 cancel.
	at int
}

// NewConfirm makes a question, "Yes" chosen.
func NewConfirm(title, text string) *Confirm {
	return &Confirm{Title: title, Text: text}
}

// confirmLabels is the three buttons, in order.
var confirmLabels = [3]string{"Yes", "No", "Cancel"}

// confirmOutcomes is what each button answers.
var confirmOutcomes = [3]Outcome{Accepted, Rejected, Cancelled}

// Draw paints the question and its buttons.
func (c *Confirm) Draw(s *term.Screen) {
	r := panel(s, c.Title, max(len([]rune(c.Text))+6, 36), 7)
	s.Text(r.X+3, r.Y+2, c.Text, Panel, r.W-6)
	x := r.X + (r.W-32)/2
	for i, label := range confirmLabels {
		x += button(s, x, r.Y+4, label, i == c.at) + 2
	}
}

// Key answers on Enter, Escape, or the first letter of a button.

// letter answers by a button's first letter.
func (c *Confirm) letter(r rune) Outcome {
	for i, label := range confirmLabels {
		if unicode.ToLower(rune(label[0])) == r {
			return confirmOutcomes[i]
		}
	}
	return Pending
}

// Prompt asks for one line of text.
type Prompt struct {
	Title, Label string
	Input        Input
}

// NewPrompt makes a prompt with a text already in it.
func NewPrompt(title, label, text string) *Prompt {
	return &Prompt{Title: title, Label: label, Input: NewInput(text)}
}

// Draw paints the label and the line being typed.
func (p *Prompt) Draw(s *term.Screen) {
	r := panel(s, p.Title, 60, 7)
	s.Text(r.X+3, r.Y+2, p.Label, Panel, r.W-6)
	p.Input.Draw(s, r.X+3, r.Y+3, r.W-6)
	s.Text(r.X+3, r.Y+5, "Enter accepts · Esc cancels", BarDim, r.W-6)
}

// Key types into the line; Enter accepts it and Escape cancels.
func (p *Prompt) Key(ev term.Event) Outcome {
	switch {
	case ev.Kind == term.EvPaste:
		p.Input.Paste(ev.Text)
	case ev.Key == term.KeyEnter:
		return Accepted
	case ev.Key == term.KeyEsc:
		return Cancelled
	default:
		p.Input.Key(ev)
	}
	return Pending
}

// Picker chooses one line from many, narrowed by what is typed.
type Picker struct {
	Title string
	Input Input
	// All is every line; the list shows those that match what is typed.
	All  []string
	list List
	// Free lets Enter accept what was typed when nothing matches: a new
	// file's name.
	Free bool
	// Match, when set, decides which lines are shown for what is typed, in
	// place of the plain every-word match: a search with rules of its own.
	Match func(query string) []string
}

// NewPicker makes a picker over some lines.
func NewPicker(title string, all []string) *Picker {
	return &Picker{Title: title, All: all, list: List{Items: all}}
}

// pickerRows is how many lines a picker shows at once.
const pickerRows = 14

// Draw paints the line being typed and the lines that match it.
func (p *Picker) Draw(s *term.Screen) {
	r := panel(s, p.Title, 70, pickerRows+6)
	rows := term.Rect{X: r.X + 2, Y: r.Y + 4, W: r.W - 4, H: r.H - 6}
	p.list.Draw(s, rows, Panel, ListChosen, true)
	p.Input.Draw(s, r.X+3, r.Y+2, r.W-6)
}

// Key types into the filter, moves the choice, accepts or cancels.
func (p *Picker) Key(ev term.Event) Outcome {
	switch {
	case ev.Kind == term.EvPaste:
		p.Input.Paste(ev.Text)
		p.narrow()
	case ev.Key == term.KeyEsc:
		return Cancelled
	case ev.Key == term.KeyEnter:
		return p.accept()
	case ev.Key != term.KeyHome && ev.Key != term.KeyEnd && p.list.Key(ev, pickerRows):
	case p.Input.Key(ev):
		p.narrow()
	}
	return Pending
}

// accept takes the chosen line — or, in a free picker with nothing
// matching, what was typed.
func (p *Picker) accept() Outcome {
	if _, ok := p.list.Chosen(); ok {
		return Accepted
	}
	if p.Free && strings.TrimSpace(p.Input.String()) != "" {
		return Accepted
	}
	return Pending
}

// narrow shows only the lines that match what is typed.
func (p *Picker) narrow() {
	if p.Match != nil {
		p.list.Set(p.Match(p.Input.String()))
	} else {
		p.list.Set(Matching(p.All, p.Input.String()))
	}
	p.list.Sel, p.list.Top = 0, 0
}

// Choice is the picked line, or what was typed when nothing matches it.
func (p *Picker) Choice() string {
	if line, ok := p.list.Chosen(); ok {
		return line
	}
	return strings.TrimSpace(p.Input.String())
}

// Reset gives the picker new lines and empties what was typed: the same
// dialog showing another directory.
func (p *Picker) Reset(title string, all []string) {
	p.Title, p.All, p.Input = title, all, Input{}
	p.list = List{Items: all}
}

// Matching is the lines containing every word of a query, in any case, in
// the order given. An empty query matches everything.
func Matching(lines []string, query string) []string {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return lines
	}
	var out []string
	for _, line := range lines {
		if containsAll(strings.ToLower(line), words) {
			out = append(out, line)
		}
	}
	return out
}

// containsAll reports whether a text contains every one of some words.
func containsAll(text string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}
