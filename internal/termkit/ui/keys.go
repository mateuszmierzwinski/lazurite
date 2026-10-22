package ui

import (
	"unicode"

	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

// What each key does to each widget, as tables: one row per key, and the
// handler a named thing that can be read and tested by itself.

// caretStep moves an input line's caret by some runes.
type caretStep struct{ by int }

func (k caretStep) press(i *Input, _ term.Event) bool {
	i.Caret = min(max(i.Caret+k.by, 0), len(i.Text))
	return false
}

// caretEdge moves an input line's caret to its start or its end.
type caretEdge struct{ end bool }

func (k caretEdge) press(i *Input, _ term.Event) bool {
	i.Caret = 0
	if k.end {
		i.Caret = len(i.Text)
	}
	return false
}

// inputErase deletes the rune at an offset from the caret: the one before
// it (Backspace) or the one under it (Delete).
type inputErase struct{ offset int }

func (k inputErase) press(i *Input, _ term.Event) bool { return i.remove(i.Caret + k.offset) }

// inputType inserts the character a key types.
type inputType struct{}

func (inputType) press(i *Input, ev term.Event) bool { return i.typed(ev) }

// inputKeys is what each key does to a line being typed.
var inputKeys = map[term.KeyCode]inputKey{
	term.KeyLeft:      caretStep{by: -1},
	term.KeyRight:     caretStep{by: 1},
	term.KeyHome:      caretEdge{},
	term.KeyEnd:       caretEdge{end: true},
	term.KeyBackspace: inputErase{offset: -1},
	term.KeyDelete:    inputErase{},
	term.KeyRune:      inputType{},
}

// Key gives the line a key and says whether it changed the text.
func (i *Input) Key(ev term.Event) bool {
	if k, ok := inputKeys[ev.Key]; ok {
		return k.press(i, ev)
	}
	return false
}

// listStep is how far one key moves a list's choice: some rows, some pages,
// or all the way to an end.
type listStep struct{ rows, pages, ends int }

// listKeys is how far each key moves a list's choice.
var listKeys = map[term.KeyCode]listStep{
	term.KeyUp:   {rows: -1},
	term.KeyDown: {rows: 1},
	term.KeyPgUp: {pages: -1},
	term.KeyPgDn: {pages: 1},
	term.KeyHome: {ends: -1},
	term.KeyEnd:  {ends: 1},
}

// Key gives the list a key and says whether it was one of the list's.
func (l *List) Key(ev term.Event, page int) bool {
	step, ok := listKeys[ev.Key]
	if !ok || ev.Kind != term.EvKey {
		return false
	}
	l.Move(step.rows + step.pages*max(page, 1) + step.ends*len(l.Items))
	return true
}

// menuBack closes the pulled-down menu, or leaves the bar.
type menuBack struct{}

func (menuBack) press(m *MenuBar, _ term.Event) string {
	m.back()
	return ""
}

// menuSide moves to the menu at one side.
type menuSide struct{ by int }

func (k menuSide) press(m *MenuBar, _ term.Event) string {
	m.At, m.Row = (m.At+k.by+len(m.Menus))%len(m.Menus), 0
	return ""
}

// menuStep moves the chosen item up or down.
type menuStep struct{ by int }

func (k menuStep) press(m *MenuBar, _ term.Event) string {
	m.step(k.by)
	return ""
}

// menuChoose takes the chosen item.
type menuChoose struct{}

func (menuChoose) press(m *MenuBar, _ term.Event) string { return m.choose() }

// menuLetter picks a menu or an item by its letter.
type menuLetter struct{}

func (menuLetter) press(m *MenuBar, ev term.Event) string { return m.letter(ev.Rune) }

// menuKeys is what each key does to the menu bar.
var menuKeys = map[term.KeyCode]menuKey{
	term.KeyEsc:   menuBack{},
	term.KeyLeft:  menuSide{by: -1},
	term.KeyRight: menuSide{by: 1},
	term.KeyDown:  menuStep{by: 1},
	term.KeyUp:    menuStep{by: -1},
	term.KeyEnter: menuChoose{},
	term.KeyRune:  menuLetter{},
}

// Key gives the bar a key. It answers the command of an item that was
// chosen, or nothing.
func (m *MenuBar) Key(ev term.Event) string {
	if k, ok := menuKeys[ev.Key]; ok && len(m.Menus) > 0 {
		return k.press(m, ev)
	}
	return ""
}

// confirmEscape cancels a question.
type confirmEscape struct{}

func (confirmEscape) press(*Confirm, term.Event) Outcome { return Cancelled }

// confirmEnter answers with the button that has the keyboard.
type confirmEnter struct{}

func (confirmEnter) press(c *Confirm, _ term.Event) Outcome { return confirmOutcomes[c.at] }

// confirmStep moves the keyboard to the button at one side.
type confirmStep struct{ by int }

func (k confirmStep) press(c *Confirm, _ term.Event) Outcome {
	c.at = (c.at + k.by + len(confirmLabels)) % len(confirmLabels)
	return Pending
}

// confirmLetter answers by a button's first letter.
type confirmLetter struct{}

func (confirmLetter) press(c *Confirm, ev term.Event) Outcome {
	return c.letter(unicode.ToLower(ev.Rune))
}

// confirmKeys is what each key does to a question.
var confirmKeys = map[term.KeyCode]confirmKey{
	term.KeyEsc:     confirmEscape{},
	term.KeyEnter:   confirmEnter{},
	term.KeyLeft:    confirmStep{by: -1},
	term.KeyBackTab: confirmStep{by: -1},
	term.KeyRight:   confirmStep{by: 1},
	term.KeyTab:     confirmStep{by: 1},
	term.KeyRune:    confirmLetter{},
}

// Key answers on Enter, Escape, or the first letter of a button.
func (c *Confirm) Key(ev term.Event) Outcome {
	if k, ok := confirmKeys[ev.Key]; ok && ev.Kind == term.EvKey {
		return k.press(c, ev)
	}
	return Pending
}
