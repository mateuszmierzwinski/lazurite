package tty

import "github.com/mateuszmierzwinski/lazurite/internal/termkit/term"

// Port is a terminal taken over, as the program drawing on it sees it. A
// program's own terminal port — an interface with these methods, declared
// where its core is — is satisfied by what Open returns.
type Port interface {
	// Events is the keys, mouse, pastes and resizes, in order. It is closed
	// when the terminal is gone.
	Events() <-chan term.Event
	// Size is how many columns and rows the terminal has now.
	Size() (w, h int)
	// Show writes the bytes of a frame.
	Show(frame []byte) error
	// Lend hands the terminal — the user's own screen, cooked input — to
	// something else until it returns, then takes it back.
	Lend(use func()) error
}
