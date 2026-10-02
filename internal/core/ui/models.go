// Package ui is what Lazurite looks like: the menu bar and its menus, the
// framed windows, the dialogs, the editor's picture of a text and the status
// line — each a small thing that draws itself on a screen and answers keys.
//
// It is the classic text-mode desktop: a grey bar, blue windows with
// double-line frames, grey dialogs that cast a shadow. Nothing here knows
// what the IDE does; the widgets are told what to show and report what was
// chosen.
package ui

import (
	"github.com/mateuszmierzwinski/lazurite/internal/core/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/lexer"
)

// The colours of everything, in one place.
var (
	Desktop = term.Style{Fg: term.Blue, Bg: term.Grey}

	Bar         = term.Style{Fg: term.Black, Bg: term.Grey}
	BarHot      = term.Style{Fg: term.Red, Bg: term.Grey}
	BarChosen   = term.Style{Fg: term.Black, Bg: term.Green}
	BarChosenHt = term.Style{Fg: term.Red, Bg: term.Green}
	BarDim      = term.Style{Fg: term.DarkGrey, Bg: term.Grey}

	Frame      = term.Style{Fg: term.White, Bg: term.Blue}
	FrameIdle  = term.Style{Fg: term.Grey, Bg: term.Blue}
	FrameMark  = term.Style{Fg: term.BrightGreen, Bg: term.Blue}
	Text       = term.Style{Fg: term.Yellow, Bg: term.Blue}
	Keyword    = term.Style{Fg: term.White, Bg: term.Blue}
	Comment    = term.Style{Fg: term.Grey, Bg: term.Blue}
	Literal    = term.Style{Fg: term.BrightCyan, Bg: term.Blue}
	Number     = term.Style{Fg: term.BrightMagenta, Bg: term.Blue}
	Declared   = term.Style{Fg: term.BrightGreen, Bg: term.Blue}
	Selection  = term.Style{Fg: term.Blue, Bg: term.Grey}
	Fault      = term.Style{Fg: term.BrightRed, Bg: term.Blue, Underline: true}
	Doubt      = term.Style{Fg: term.Yellow, Bg: term.Blue, Underline: true}
	BreakLine  = term.Style{Fg: term.White, Bg: term.Red}
	ResumeLine = term.Style{Fg: term.Black, Bg: term.Cyan}

	Pane       = term.Style{Fg: term.Black, Bg: term.Cyan}
	PaneFrame  = term.Style{Fg: term.White, Bg: term.Cyan}
	PaneChosen = term.Style{Fg: term.White, Bg: term.Blue}
	PaneDim    = term.Style{Fg: term.Blue, Bg: term.Cyan}

	Panel        = term.Style{Fg: term.Black, Bg: term.Grey}
	PanelFrame   = term.Style{Fg: term.White, Bg: term.Grey}
	Field        = term.Style{Fg: term.White, Bg: term.Blue}
	Button       = term.Style{Fg: term.Black, Bg: term.Green}
	ButtonChosen = term.Style{Fg: term.White, Bg: term.Green}
	ListChosen   = term.Style{Fg: term.White, Bg: term.Green}
	Shade        = term.Style{Fg: term.DarkGrey, Bg: term.Black}

	Popup       = term.Style{Fg: term.Black, Bg: term.Cyan}
	PopupChosen = term.Style{Fg: term.White, Bg: term.Black}
)

// Item is one line of a menu. An item with no label is a separating rule.
type Item struct {
	// Label is what is shown; a letter between tildes ("~O~pen") is the one
	// that picks the item.
	Label string
	// Keys is the shortcut shown at the right ("F3"); it is only text.
	Keys string
	// Command is what choosing the item asks for.
	Command string
}

// Menu is one pull-down menu.
type Menu struct {
	Title string
	Items []Item
}

// MenuBar is the bar across the top and the menu pulled down from it.
type MenuBar struct {
	Menus []Menu
	// Active says the bar has the keyboard; Open says a menu is pulled
	// down. At is which menu, Row which of its items.
	Active, Open bool
	At, Row      int
}

// Outcome is what a key did to a dialog.
type Outcome int

// The outcomes.
const (
	// Pending: the dialog is still up.
	Pending Outcome = iota
	// Accepted: OK, Yes, or a choice made.
	Accepted
	// Rejected: No — a refusal that is not a cancellation.
	Rejected
	// Cancelled: Escape.
	Cancelled
)

// Input is one line of text being typed.
type Input struct {
	Text  []rune
	Caret int
}

// List is a column of lines, one of them chosen.
type List struct {
	Items    []string
	Sel, Top int
}

// MaxInput bounds what an input line holds.
const MaxInput = 1024

// Mark is a stretch of text something is wrong with.
type Mark struct {
	From, To edit.Pos
	// Severe is an error; otherwise a warning.
	Severe bool
}

// Picture is everything the editor's drawing needs besides the text: its
// colours, what is wrong with it, and where the debugger is.
type Picture struct {
	// Spans is the syntax colouring, a slice per line.
	Spans [][]lexer.Span
	Marks []Mark
	// Breaks is the lines carrying a breakpoint; Resume the line the
	// debugger rests on, or -1.
	Breaks map[int]bool
	Resume int
	// Free leaves the view where it was scrolled to, even with the caret
	// out of it; otherwise the caret is brought into view.
	Free bool
}

// Hint is one key named on the status line.
type Hint struct {
	Key, Label string
}

// Title is what a window's frame says.
type Title struct {
	Name string
	// Number is the window's number, shown at the right; zero shows none.
	Number int
	// Foot is shown in the bottom edge: the caret's line and column.
	Foot string
	// Changed marks a text with unsaved edits.
	Changed bool
	// Active is the window that has the keyboard.
	Active bool
}
