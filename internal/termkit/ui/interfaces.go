package ui

import "github.com/mateuszmierzwinski/lazurite/internal/termkit/term"

// Dialog is anything shown over the desktop that takes the keyboard until it
// is answered.
type Dialog interface {
	Draw(s *term.Screen)
	Key(ev term.Event) Outcome
}

// inputKey is what one key does to a line being typed; it says whether the
// text changed.
type inputKey interface {
	press(i *Input, ev term.Event) bool
}

// menuKey is what one key does to the menu bar; it answers the command of
// an item that was chosen, or nothing.
type menuKey interface {
	press(m *MenuBar, ev term.Event) string
}

// confirmKey is what one key does to a question.
type confirmKey interface {
	press(c *Confirm, ev term.Event) Outcome
}
