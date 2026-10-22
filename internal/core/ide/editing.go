package ide

import (
	"unicode"

	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/ui"
)

// Typing, the clipboard and the mouse.

// typed puts a typed character into the text and keeps the completion list
// in step with the word.
func (a *App) typed(r rune) {
	ed := a.editor()
	if ed == nil || a.focus != focusText {
		return
	}
	ed.Type(r)
	a.followTyping(r)
}

// enter breaks the line.
func (a *App) enter() {
	if ed := a.editor(); ed != nil {
		ed.Enter()
	}
}

// paste puts a text pasted into the terminal into the text being edited.
func (a *App) paste(text string) {
	a.popup = nil
	if ed := a.editor(); ed != nil && a.focus == focusText && !a.menu.Active {
		ed.Insert(text)
	}
}

// copy puts the selection on the clipboard: the IDE's own, and the
// terminal's where it has one.
func (a *App) copy() {
	ed := a.editor()
	if ed == nil || !ed.Selected() {
		return
	}
	a.clip = ed.SelectedText()
	a.offerClip()
}

// cut removes the selection and puts it on the clipboard.
func (a *App) cut() {
	ed := a.editor()
	if ed == nil || !ed.Selected() {
		return
	}
	a.clip = ed.Cut()
	a.offerClip()
}

// offerClip asks the terminal to put the IDE's clipboard on the system's.
func (a *App) offerClip() {
	if seq := term.Clipboard(a.clip); seq != nil {
		_ = a.deps.Terminal.Show(seq)
	}
}

// pasteClip puts the IDE's clipboard into the text.
func (a *App) pasteClip() {
	if ed := a.editor(); ed != nil && a.clip != "" {
		ed.Insert(a.clip)
	}
}

// escape takes the keyboard back to the text.
func (a *App) escape() {
	a.focus = focusText
}

// enterMenu gives the menu bar the keyboard.
func (a *App) enterMenu() {
	a.menu.Enter()
}

// zoom hides the bottom window, or shows it again.
func (a *App) zoom() {
	a.pane.shown = !a.pane.shown
	if !a.pane.shown && a.focus == focusPane {
		a.focus = focusText
	}
}

// page is how many lines a page key moves: the height of the text's window
// less one line of overlap.
func (a *App) page() int {
	return max(a.layout().text.H-3, 1)
}

// wheelLines is how far one notch of the mouse wheel scrolls.
const wheelLines = 3

// mouse gives a mouse event to what is under it.
func (a *App) mouse(ev term.Event) {
	if ev.Y == 0 || a.menu.Active {
		command, _ := a.menu.Click(a.screen, ev.X, ev.Y)
		a.Do(command)
		return
	}
	at := a.layout()
	switch {
	case inside(at.bottom, ev.X, ev.Y):
		a.paneMouse(ev, at.bottom)
	case inside(at.tree, ev.X, ev.Y):
		a.treeMouse(ev, at.tree)
	case inside(at.text, ev.X, ev.Y):
		a.textMouse(ev, at.text)
	}
}

// inside reports whether a cell is in a rectangle.
func inside(r term.Rect, x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// textMouse is the mouse over the text: a press puts the caret, a drag
// selects, the wheel scrolls.
func (a *App) textMouse(ev term.Event, window term.Rect) {
	ed := a.editor()
	if ed == nil {
		return
	}
	a.popup = nil
	at := ui.PosAt(ed, inner(window), ev.X, ev.Y)
	switch ev.Button {
	case term.MouseLeft:
		a.focus = focusText
		ed.Goto(at)
	case term.MouseDrag:
		ed.Select(ed.Anchor, at)
	case term.WheelUp:
		ed.Scroll(-wheelLines)
	case term.WheelDown:
		ed.Scroll(wheelLines)
	}
}

// inner is the inside of a framed window.
func inner(r term.Rect) term.Rect {
	return term.Rect{X: r.X + 1, Y: r.Y + 1, W: max(r.W-2, 0), H: max(r.H-2, 0)}
}

// identifier reports whether a character can be part of a name.
func identifier(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
