// Package term is the terminal as a text-mode program sees it: a grid of coloured
// cells to draw on, the bytes that make a real terminal show it, and the
// keys and mouse a real terminal sends back.
//
// It is pure: it turns cells into bytes and bytes into events, and touches
// no file descriptor. The adapter that owns the real terminal is
// `adapters/tty`.
package term

// Color is one of the sixteen ANSI colours every terminal has.
type Color uint8

// The sixteen colours, in ANSI order.
const (
	Black Color = iota
	Red
	Green
	Brown
	Blue
	Magenta
	Cyan
	Grey
	DarkGrey
	BrightRed
	BrightGreen
	Yellow
	BrightBlue
	BrightMagenta
	BrightCyan
	White
)

// Style is how a cell is drawn.
type Style struct {
	Fg, Bg    Color
	Underline bool
}

// Cell is one character of the screen.
type Cell struct {
	R rune
	S Style
}

// Rect is a rectangle of cells; W and H may be zero (nothing).
type Rect struct {
	X, Y, W, H int
}

// Screen is the picture being drawn: W×H cells, and where the cursor is.
type Screen struct {
	W, H  int
	Cells []Cell
	// CurX and CurY are where the terminal's own cursor is put; it is
	// hidden when CursorOn is false.
	CurX, CurY int
	CursorOn   bool
}

// The largest screen drawn: a terminal that reports more is treated as this
// big, so a hostile or broken size cannot ask for an unbounded grid.
const (
	MaxWidth  = 500
	MaxHeight = 200
)

// KeyCode is which key an event is.
type KeyCode int

// The keys. KeyRune is any key that types a character; the character is in
// Event.Rune.
const (
	KeyNone KeyCode = iota
	KeyRune
	KeyEnter
	KeyTab
	KeyBackTab
	KeyBackspace
	KeyEsc
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDn
	KeyInsert
	KeyDelete
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

// Mods is the modifier keys held with a key.
type Mods uint8

// The modifiers.
const (
	Shift Mods = 1 << iota
	Alt
	Ctrl
)

// Kind is what sort of thing happened.
type Kind int

// The kinds of event.
const (
	// EvKey is a key pressed.
	EvKey Kind = iota
	// EvMouse is a mouse button pressed, released or dragged, or the wheel.
	EvMouse
	// EvPaste is text pasted into the terminal, whole.
	EvPaste
	// EvResize is the terminal changing size.
	EvResize
)

// Button is what the mouse did.
type Button int

// The mouse's doings.
const (
	MouseLeft Button = iota
	MouseRelease
	MouseDrag
	WheelUp
	WheelDown
	MouseOther
)

// Event is one thing the terminal reported.
type Event struct {
	Kind Kind
	Key  KeyCode
	Rune rune
	Mods Mods
	// X and Y are the cell a mouse event happened at, from zero; for a
	// resize, the new width and height.
	X, Y   int
	Button Button
	// Text is what was pasted.
	Text string
}

// MaxPaste bounds one paste: a terminal can be fed megabytes, and an editor
// that took them as one event would stall on it.
const MaxPaste = 1 << 20
