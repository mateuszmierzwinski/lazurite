package ide

import (
	"errors"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

func TestFindSelectsTheNextOccurrenceAndFindAgainGoesOn(t *testing.T) {
	r := start(t).open("main.go")
	r.ctrl('f').sees("Find", "Text to find")
	r.write("main").press(term.KeyEnter)
	if from, _ := r.app.editor().Selection(); from != (edit.Pos{Line: 0, Col: 8}) {
		t.Fatalf("found at %+v", from)
	}
	r.ctrl('l')
	if from, _ := r.app.editor().Selection(); from != (edit.Pos{Line: 4, Col: 5}) {
		t.Fatalf("found again at %+v", from)
	}
	r.ctrl('f').sees("main")
	r.ctrl('a')
	for range "main" {
		r.press(term.KeyBackspace)
	}
	r.write("absent").press(term.KeyEnter).sees("Not found: absent")
	r.ctrl('f').press(term.KeyEsc)
}

func TestReplaceChangesEveryOccurrenceAsOneStepOfUndo(t *testing.T) {
	r := start(t).open("main.go")
	r.app.Do("replace")
	r.write("main").press(term.KeyEnter).sees("Replace every \"main\" with")
	r.write("start").press(term.KeyEnter).sees("Replaced 2", "package start", "func start()")
	r.ctrl('z').sees("package main", "func main()")
	r.app.Do("replace")
	r.ctrl('a')
	for range "main" {
		r.press(term.KeyBackspace)
	}
	r.write("zzz").press(term.KeyEnter).write("y").press(term.KeyEnter).sees("Not found: zzz")
	r.app.Do("replace")
	r.press(term.KeyEnter, term.KeyEsc)
}

func TestGoToLineTakesALineAndAColumn(t *testing.T) {
	r := start(t).open("main.go")
	r.ctrl('g').press(term.KeyBackspace).write("6:3").press(term.KeyEnter)
	if r.app.editor().Caret != (edit.Pos{Line: 5, Col: 2}) {
		t.Fatalf("at %+v", r.app.editor().Caret)
	}
	r.ctrl('g').press(term.KeyBackspace).write("x").press(term.KeyEnter).sees("Not a line number: x")
}

func TestOpenBrowsesDirectoriesAndOpensWhatIsChosen(t *testing.T) {
	r := start(t).open("main.go")
	r.press(term.KeyF3).sees("Open — .", "../", "shapes/", "go.mod", "main.go")
	r.write("sha").press(term.KeyEnter).sees("Open — shapes", "shapes.go")
	r.write("shapes").press(term.KeyEnter).sees(" shapes/shapes.go ", "func Square")
	r.ctrl('o').sees("Open — shapes")
	r.press(term.KeyEnter).sees("Open — .", "notes.txt")
	r.write("brand_new.go").press(term.KeyEnter).sees(" brand_new.go ")
	r.files.failing[root] = errors.New("gone")
	r.app.browse(root)
	r.sees("gone")
}

func TestOpenInProjectListsEveryFileOfEveryPackage(t *testing.T) {
	r := start(t)
	r.ctrl('p').sees("Open in project", "go.mod", "main.go", "shapes/shapes.go")
	r.write("squ shapes").lacks("Open in project │")
	for range "squ shapes" {
		r.press(term.KeyBackspace)
	}
	r.write("es/").press(term.KeyEnter).sees(" shapes/shapes.go ")
	r.files.failing[root] = errors.New("unreadable")
	r.ctrl('p').sees("unreadable")
}

func TestGoToDefinitionJumpsIntoAnotherPackage(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.place, r.intel.found = Place{Path: root + "/shapes/shapes.go", At: edit.Pos{Line: 2, Col: 5}}, true
	r.press(term.KeyF12).sees(" shapes/shapes.go ", "╚═ 3:6 ═")
	r.intel.found = false
	r.press(term.KeyF12).sees("No definition found")
}

func TestDescribeShowsWhatIsKnownAboutASymbol(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.about = "func fmt.Println(a ...any) (n int, err error)\n\nPrintln formats using the default formats for its operands and writes to standard output."
	r.ctrl('d').sees("Symbol", "func fmt.Println", "Println formats using the default formats")
	r.press(term.KeyEnter)
	r.intel.about = "  "
	r.ctrl('d').sees("Nothing is known about this")
	r.ctrl('n').ctrl('d').press(term.KeyF12).ctrl(' ')
}

func TestHelpAndAboutAreShownAndDismissed(t *testing.T) {
	r := start(t)
	r.press(term.KeyF1).sees("Keys", "F9 build all", "Ctrl+F8 breakpoint")
	r.press(term.KeyEnter)
	r.app.Do("about")
	r.sees("About", "Lazurite", "A console IDE for Go", "version test")
	r.send(term.Event{Kind: term.EvMouse, Button: term.MouseLeft, X: 1, Y: 1}).sees("About")
	r.press(term.KeyEsc).lacks("A console IDE for Go")
	r.app.deps.Version = ""
	r.app.Do("about")
	r.sees("development build")
}

func TestTheMenuBarRunsCommandsByKeyLetterAndMouse(t *testing.T) {
	r := start(t).open("main.go")
	r.press(term.KeyF10).sees("Enter Choose", "Esc Back")
	r.press(term.KeyEnter).sees("Open in project…", "Ctrl+P", "Exit")
	r.write("n")
	if len(r.app.docs) != 2 {
		t.Fatal("File → New did not open a text")
	}
	r.alt('w').sees("Zoom", "Messages").write("m")
	if !r.app.pane.shown || r.app.pane.kind != paneMessages {
		t.Fatal("Window → Messages did not show the page")
	}
	r.send(term.Event{Kind: term.EvMouse, Button: term.MouseLeft, X: 3, Y: 0}).sees("Save as…")
	r.send(term.Event{Kind: term.EvMouse, Button: term.MouseLeft, X: 3, Y: 2})
	if len(r.app.docs) != 3 {
		t.Fatal("a press on File → New did not open a text")
	}
	r.alt('f').press(term.KeyEsc, term.KeyEsc).send(term.Event{Kind: term.EvPaste, Text: "x"})
}

func TestEveryMenuItemNamesACommandAndNoTwoShareALetter(t *testing.T) {
	for _, menu := range menus() {
		letters := map[rune]string{}
		for _, item := range menu.Items {
			if item.Label == "" {
				continue
			}
			if _, ok := commands[item.Command]; !ok {
				t.Errorf("%s → %s names no command: %q", menu.Title, item.Label, item.Command)
			}
			key := []rune(item.Label[indexOfTilde(item.Label)+1:])[0]
			if other, taken := letters[key]; taken {
				t.Errorf("%s: %q and %q share the letter %q", menu.Title, other, item.Label, key)
			}
			letters[key] = item.Label
		}
	}
	for chord, name := range keymap {
		if _, ok := commands[name]; !ok {
			t.Errorf("%+v is bound to %q, which is no command", chord, name)
		}
	}
}

func indexOfTilde(label string) int {
	for i, r := range label {
		if r == '~' {
			return i
		}
	}
	return -1
}

func TestACommandThatDoesNotExistDoesNothing(t *testing.T) {
	r := start(t)
	r.app.Do("no.such.command")
	r.app.Do("")
}
