package ide

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/ui"
)

// The texts that are open: opening, switching, saving, closing.

// doc is the text being edited; nil when none is open.
func (a *App) doc() *document {
	if a.at < 0 || a.at >= len(a.docs) {
		return nil
	}
	return a.docs[a.at]
}

// editor is the editor of the text being edited; nil when none is open.
func (a *App) editor() *edit.Editor {
	if d := a.doc(); d != nil {
		return d.ed
	}
	return nil
}

// add makes a text the one being edited.
func (a *App) add(d *document) {
	a.docs = append(a.docs, d)
	a.at = len(a.docs) - 1
	a.focus = focusText
}

// show switches to the text with an index, when there is one.
func (a *App) show(i int) {
	if i < 0 || i >= len(a.docs) {
		return
	}
	a.at, a.focus = i, focusText
}

// newDocument opens an empty text that has no file yet.
//
// It is given a place in the project all the same — a name nothing on the
// disk has — because the language server completes a text by where it
// would live: without one, a new file would be the one place completion
// does not work. The first save asks where it should really go.
func (a *App) newDocument() {
	if len(a.docs) >= maxDocuments {
		a.fail(ErrTooManyDocuments)
		return
	}
	path := a.freeName()
	a.add(&document{path: path, name: displayName(a.root, path), ed: edit.NewEditor(""), unnamed: true})
	a.intel.Open(path, "")
}

// freeName is a name in the project for a text with no file: the next
// "nonameNN.go" that neither the disk nor an open text has.
func (a *App) freeName() string {
	for {
		a.untitled++
		path := filepath.Join(a.root, untitledName(a.untitled))
		if _, err := a.deps.Files.Read(path); errors.Is(err, fs.ErrNotExist) && !a.isOpen(path) {
			return path
		}
		if a.untitled > maxDocuments*4 {
			return path
		}
	}
}

// isOpen reports whether a file is among the open texts.
func (a *App) isOpen(path string) bool {
	for _, d := range a.docs {
		if d.path == path {
			return true
		}
	}
	return false
}

// Open opens a file, or switches to it when it is open already. A file
// that does not exist yet is opened empty and made by the first save; until
// something is typed it is not a text with unsaved edits, and leaving it
// asks nothing.
func (a *App) Open(path string) error {
	path = filepath.Clean(path)
	for i, d := range a.docs {
		if d.path == path {
			a.show(i)
			return nil
		}
	}
	if len(a.docs) >= maxDocuments {
		return ErrTooManyDocuments
	}
	text, err := a.deps.Files.Read(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	a.add(&document{path: path, name: displayName(a.root, path), ed: edit.NewEditor(text)})
	a.intel.Open(path, text)
	return nil
}

// open opens a file for a command, putting a failure on the status line.
func (a *App) open(path string) {
	if err := a.Open(path); err != nil {
		a.fail(err)
	}
}

// jump opens a file and puts the caret at a place in it.
func (a *App) jump(p Place) {
	if err := a.Open(p.Path); err != nil {
		a.fail(err)
		return
	}
	a.editor().Goto(p.At)
}

// sync tells the language server about the edits made to the text being
// edited since it was last told.
func (a *App) sync() {
	d := a.doc()
	if d == nil || d.sentRev == d.ed.Buf.Rev() {
		return
	}
	d.sentRev = d.ed.Buf.Rev()
	a.intel.Change(d.path, d.ed.Buf.Text())
}

// save writes the text being edited, asking for a name when it has none.
func (a *App) save() {
	d := a.doc()
	if d == nil {
		return
	}
	if d.unnamed {
		a.saveAs()
		return
	}
	if a.write(d) {
		a.say("Saved " + d.name)
	}
}

// write cleans a text up and writes it to its file. It reports whether the
// file was written.
func (a *App) write(d *document) bool {
	a.tidy(d)
	text := d.ed.Buf.Text()
	if err := a.deps.Files.Write(d.path, text); err != nil {
		a.fail(err)
		return false
	}
	d.savedRev, d.sentRev = d.ed.Buf.Rev(), d.ed.Buf.Rev()
	a.intel.Change(d.path, text)
	a.intel.Save(d.path, text)
	a.rebuildTree()
	return true
}

// tidy cleans a Go text up in place: formatted, its imports made to match
// what it uses. A text that does not parse is left as it is, and the
// status line says why.
func (a *App) tidy(d *document) {
	if !strings.HasSuffix(d.path, ".go") {
		return
	}
	old := d.ed.Buf.Text()
	text, err := a.deps.Tidier.Tidy(d.path, old)
	if err != nil {
		a.say("Not cleaned up: " + oneLine(err.Error()))
		return
	}
	if text == old {
		return
	}
	caret := d.ed.Caret
	caret.Line = relocate(strings.Split(old, "\n"), strings.Split(text, "\n"), caret.Line)
	d.ed.Buf.SetText(text)
	d.ed.Goto(caret)
}

// cleanUp cleans the text being edited up without saving it.
func (a *App) cleanUp() {
	d := a.doc()
	if d == nil {
		return
	}
	if !strings.HasSuffix(d.name, ".go") {
		a.say("Only Go files are cleaned up")
		return
	}
	a.say("Cleaned up")
	a.tidy(d)
}

// saveAs asks for a name and saves the text being edited under it.
func (a *App) saveAs() {
	d := a.doc()
	if d == nil {
		return
	}
	p := ui.NewPrompt("Save as", "File name", d.path)
	a.ask(p, func(o ui.Outcome) {
		if o == ui.Accepted && strings.TrimSpace(p.Input.String()) != "" {
			a.rename(d, a.absolute(p.Input.String()))
		}
	})
}

// rename gives a text a new file and writes it there.
func (a *App) rename(d *document, path string) {
	a.intel.Close(d.path)
	d.path, d.name, d.unnamed = path, displayName(a.root, path), false
	d.spansRev = -1
	a.intel.Open(path, d.ed.Buf.Text())
	if a.write(d) {
		a.say("Saved " + d.name)
	}
}

// absolute is a path as typed, made whole: one that is not absolute is
// inside the project.
func (a *App) absolute(typed string) string {
	typed = strings.TrimSpace(typed)
	if filepath.IsAbs(typed) {
		return filepath.Clean(typed)
	}
	return filepath.Join(a.root, typed)
}

// saveEverything saves every changed text that has a file, for the menu.
func (a *App) saveEverything() {
	if a.saveAll() {
		a.say("Saved all")
	}
}

// saveAll saves every changed text that has a file, and reports whether all
// of them were written. A build reads the disk, so this runs before one.
func (a *App) saveAll() bool {
	for _, d := range a.docs {
		if !d.unnamed && d.changed() && !a.write(d) {
			return false
		}
	}
	return true
}

// closeDocument closes the text being edited, asking first when it has
// edits that are not saved.
func (a *App) closeDocument() {
	a.closeThen(nothingMore)
}

// nothingMore is what follows a close that is only a close.
func nothingMore() {
	// Closing was all that was asked.
}

// closeThen closes the text being edited and then goes on; with unsaved
// edits it asks, and goes on only when the answer was not to cancel.
func (a *App) closeThen(then func()) {
	d := a.doc()
	if d == nil {
		then()
		return
	}
	if !d.changed() {
		a.drop()
		then()
		return
	}
	a.ask(ui.NewConfirm("Close", "Save changes to "+ui.Clip(d.name, 30)+"?"), func(o ui.Outcome) {
		a.closeAnswered(d, o, then)
	})
}

// closeAnswered acts on the answer to "save changes?".
func (a *App) closeAnswered(d *document, o ui.Outcome, then func()) {
	switch o {
	case ui.Accepted:
		if d.unnamed {
			a.saveAs()
			return
		}
		if !a.write(d) {
			return
		}
	case ui.Cancelled:
		return
	}
	a.drop()
	then()
}

// drop removes the text being edited from the open ones.
func (a *App) drop() {
	d := a.doc()
	if d == nil {
		return
	}
	a.intel.Close(d.path)
	a.docs = append(a.docs[:a.at], a.docs[a.at+1:]...)
	a.at = min(a.at, len(a.docs)-1)
	a.at = max(a.at, 0)
}

// leave quits, closing every text first: each with unsaved edits asks, and
// a cancelled question cancels the leaving.
func (a *App) leave() {
	a.settleAllThen(func() { a.quit = true })
}

// settleAllThen asks about every text with unsaved edits, one after
// another, and goes on when none is left; a cancelled question ends it
// there.
func (a *App) settleAllThen(then func()) {
	for i, d := range a.docs {
		if d.changed() {
			a.at = i
			a.closeThen(func() { a.settleAllThen(then) })
			return
		}
	}
	then()
}

// untitledName is the name of the n-th text that has no file.
func untitledName(n int) string {
	return "noname" + pad2(n) + ".go"
}

// pad2 is a number in at least two digits.
func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
