package ide

import (
	"errors"
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

func TestAnEmptyDesktopShowsTheBarTheBackdropAndTheKeys(t *testing.T) {
	r := start(t)
	r.sees("File  Edit  Search  Run  Build  Debug  Window  Help", "░░░", " demo ", "▸ shapes/", "main.go", "Enter Open")
	r.write("typing with nothing open does nothing")
	r.press(term.KeyEnter, term.KeyBackspace, term.KeyF2, term.KeyF8).ctrl('k').ctrl('c').ctrl('x').ctrl('v')
	if r.app.editor() != nil {
		t.Fatal("a key opened a text")
	}
}

func TestAFileIsOpenedInAFramedWindowWithItsNameAndNumber(t *testing.T) {
	r := start(t).open("main.go")
	r.sees("╔═[■]", " main.go ", "package main", "fmt.Println(\"hi\")", "╚═ 1:1 ═")
	if got := r.intel.log; len(got) != 1 || got[0] != "open main.go" {
		t.Fatalf("the language server was told %v", got)
	}
}

func TestTypingEditsTheTextMarksItChangedAndTellsTheLanguageServer(t *testing.T) {
	r := start(t).open("main.go")
	r.caret(5, 19).press(term.KeyEnter).write("x := 1")
	if !strings.Contains(r.text(), "\tfmt.Println(\"hi\")\n\tx := 1\n}") {
		t.Fatalf("got %q", r.text())
	}
	r.sees(" main.go * ", "╚═ 7:8 ═")
	if r.intel.texts[root+"/main.go"] != r.text() {
		t.Fatal("the language server does not have the text as typed")
	}
}

func TestTheCaretMovesAndSelectsWithTheKeys(t *testing.T) {
	r := start(t).open("main.go")
	r.press(term.KeyDown, term.KeyDown, term.KeyEnd)
	if r.app.editor().Caret != (edit.Pos{Line: 2, Col: 12}) {
		t.Fatalf("at %+v", r.app.editor().Caret)
	}
	r.chord(term.KeyHome, term.Shift)
	if r.app.editor().SelectedText() != "import \"fmt\"" {
		t.Fatalf("selected %q", r.app.editor().SelectedText())
	}
	r.chord(term.KeyEnd, term.Ctrl).chord(term.KeyHome, term.Ctrl|term.Shift)
	if r.app.editor().SelectedText() != mainSource {
		t.Fatal("Ctrl+Shift+Home did not select to the start")
	}
	r.press(term.KeyPgDn, term.KeyPgUp, term.KeyUp, term.KeyRight, term.KeyLeft)
	r.chord(term.KeyRight, term.Ctrl).chord(term.KeyLeft, term.Ctrl|term.Shift)
	if r.app.editor().SelectedText() != "package " {
		t.Fatalf("a word was selected as %q", r.app.editor().SelectedText())
	}
	r.chord(term.KeyDown, term.Shift).chord(term.KeyPgDn, term.Shift).chord(term.KeyPgUp, term.Shift).chord(term.KeyUp, term.Shift)
}

func TestCutCopyPasteUndoRedoAndSelectAll(t *testing.T) {
	r := start(t).open("notes.txt")
	r.ctrl('a').ctrl('c')
	if got := string(r.terminal.frames[len(r.terminal.frames)-1]); !strings.HasPrefix(got, "\x1b]52;c;") {
		t.Fatalf("the terminal's clipboard was not offered the text: %q", got)
	}
	r.press(term.KeyEnd).ctrl('v')
	if r.text() != "plain text\nplain text\n" {
		t.Fatalf("copy and paste gave %q", r.text())
	}
	r.ctrl('a').ctrl('x')
	if r.text() != "" {
		t.Fatalf("cut left %q", r.text())
	}
	r.ctrl('z')
	if r.text() != "plain text\nplain text\n" {
		t.Fatalf("undo gave %q", r.text())
	}
	r.ctrl('y').chord(term.KeyInsert, term.Shift)
	if r.text() != "plain text\nplain text\n" {
		t.Fatalf("redo and Shift+Insert gave %q", r.text())
	}
	r.press(term.KeyTab, term.KeyDelete, term.KeyBackspace, term.KeyBackspace)
}

func TestAPasteFromTheTerminalIsInsertedAsTextAndNeverRunAsKeys(t *testing.T) {
	r := start(t).open("notes.txt")
	r.send(term.Event{Kind: term.EvPaste, Text: "one\x1b[21~two\x18\n"})
	if !strings.HasPrefix(r.text(), "one\x1b[21~two\x18\n") {
		t.Fatalf("got %q", r.text())
	}
	if r.app.menu.Active || r.app.Quitting() {
		t.Fatal("a pasted key sequence was acted on")
	}
	r.lacks("\x1b")
}

func TestSavingCleansUpWritesAndSaysSo(t *testing.T) {
	r := start(t).open("main.go")
	r.tidier.tidy = func(_, text string) (string, error) {
		return strings.Replace(text, "import \"fmt\"", "import (\n\t\"fmt\"\n\t\"os\"\n)", 1), nil
	}
	r.caret(5, 0).write("// x").press(term.KeyF2)
	saved := r.files.files[root+"/main.go"]
	if !strings.Contains(saved, "\"os\"") || !strings.Contains(saved, "// x") {
		t.Fatalf("saved %q", saved)
	}
	if got := r.app.editor().Caret; got != (edit.Pos{Line: 8, Col: 4}) {
		t.Fatalf("the caret did not follow its line down past the new imports: %+v", got)
	}
	r.sees("Saved main.go").lacks(" main.go * ")
	if last := r.intel.log[len(r.intel.log)-1]; last != "save main.go" {
		t.Fatalf("the language server was last told %q", last)
	}
}

func TestATextThatDoesNotParseIsSavedAsItIsAndSaysWhy(t *testing.T) {
	r := start(t).open("main.go")
	r.tidier.tidy = func(string, string) (string, error) { return "", errors.New("main.go:3:1: expected declaration\nmore") }
	r.write("oops ").ctrl('s')
	if !strings.HasPrefix(r.files.files[root+"/main.go"], "oops package main") {
		t.Fatal("the text was not saved")
	}
	r.ctrl('k').sees("Not cleaned up: main.go:3:1: expected declaration")
}

func TestOnlyGoFilesAreCleanedUp(t *testing.T) {
	r := start(t).open("notes.txt")
	r.write("x").press(term.KeyF2)
	if len(r.tidier.paths) != 0 {
		t.Fatal("a text file was cleaned up")
	}
	r.ctrl('k').sees("Only Go files are cleaned up")
}

func TestCleanUpWorksOnATextThatHasNoFileYet(t *testing.T) {
	r := start(t)
	r.tidier.tidy = func(_, text string) (string, error) { return strings.TrimSpace(text) + "\n", nil }
	r.ctrl('n').write("  package x  ").ctrl('k')
	if r.text() != "package x\n" {
		t.Fatalf("got %q", r.text())
	}
	r.sees("noname01.go", "Cleaned up")
}

func TestAFailedWriteIsReportedAndTheTextStaysChanged(t *testing.T) {
	r := start(t).open("notes.txt")
	r.files.failing[root+"/notes.txt"] = errors.New("disk full")
	r.write("x").press(term.KeyF2).sees("disk full", " notes.txt * ")
}

func TestANewTextIsNamedWhenItIsFirstSaved(t *testing.T) {
	r := start(t)
	r.ctrl('n').write("package y").press(term.KeyF2).sees("Save as", "File name")
	for range "noname01.go" {
		r.press(term.KeyBackspace)
	}
	r.write("pkg/y.go").press(term.KeyEnter)
	if r.files.files[root+"/pkg/y.go"] != "package y" {
		t.Fatalf("files: %v", r.files.writes)
	}
	r.sees(" pkg/y.go ", "Saved pkg/y.go")
	if got := r.intel.log; got[len(got)-3] != "open y.go" {
		t.Fatalf("the language server was told %v", got)
	}
}

func TestSaveAsMovesATextToANewFile(t *testing.T) {
	r := start(t).open("notes.txt")
	r.app.Do("save.as")
	r.ctrl('a')
	for range root + "/notes.txt" {
		r.press(term.KeyBackspace)
	}
	r.write("/elsewhere/copy.txt").press(term.KeyEnter)
	if r.files.files["/elsewhere/copy.txt"] != "plain text\n" {
		t.Fatal("not written")
	}
	r.sees(" /elsewhere/copy.txt ")
	r.app.Do("save.as")
	r.press(term.KeyEsc)
	r.app.Do("save.all")
	r.sees("Saved all")
}

func TestClosingAChangedTextAsksAndEachAnswerIsHonoured(t *testing.T) {
	r := start(t).open("notes.txt")
	r.write("x").chord(term.KeyF3, term.Alt).sees("Save changes to notes.txt?")
	r.press(term.KeyEsc)
	if r.app.doc() == nil {
		t.Fatal("Cancel closed the text")
	}
	r.ctrl('w').write("n")
	if r.app.doc() != nil || r.files.files[root+"/notes.txt"] != "plain text\n" {
		t.Fatal("No did not close without saving")
	}
	r.open("notes.txt").write("y").ctrl('w').write("y")
	if r.app.doc() != nil || r.files.files[root+"/notes.txt"] != "yplain text\n" {
		t.Fatal("Yes did not save and close")
	}
	if last := r.intel.log[len(r.intel.log)-1]; last != "close notes.txt" {
		t.Fatalf("the language server was last told %q", last)
	}
	r.ctrl('w')
}

func TestClosingAChangedTextWithNoFileAsksForAName(t *testing.T) {
	r := start(t)
	r.ctrl('n').write("x").ctrl('w').write("y").sees("Save as")
	r.press(term.KeyEsc)
	r.files.readOnly[root+"/notes.txt"] = true
	r.open("notes.txt").write("z").ctrl('w').write("y")
	if r.app.doc() == nil {
		t.Fatal("a text that could not be saved was closed")
	}
}

func TestQuittingAsksAboutEveryChangedTextAndCancelStaysIn(t *testing.T) {
	r := start(t).open("main.go").open("notes.txt")
	r.write("a").chord(term.KeyF6, 0).write("b")
	r.alt('x').sees("Save changes to")
	r.press(term.KeyEsc)
	if r.app.Quitting() {
		t.Fatal("Cancel quit")
	}
	r.ctrl('q').write("n").write("n")
	if !r.app.Quitting() || len(r.files.writes) != 0 {
		t.Fatalf("quitting: %v, writes %v", r.app.Quitting(), r.files.writes)
	}
}

func TestQuittingWithNothingChangedJustQuits(t *testing.T) {
	r := start(t).open("main.go")
	r.alt('x')
	if !r.app.Quitting() {
		t.Fatal("did not quit")
	}
}

func TestOpeningAFileTwiceSwitchesToItAndTooManyAreRefused(t *testing.T) {
	r := start(t).open("main.go").open("notes.txt").open("main.go")
	if len(r.app.docs) != 2 || r.app.at != 0 {
		t.Fatalf("%d texts, at %d", len(r.app.docs), r.app.at)
	}
	for len(r.app.docs) < maxDocuments {
		r.ctrl('n')
	}
	r.ctrl('n').sees("too many files are open")
	if err := r.app.Open(root + "/shapes/shapes.go"); !errors.Is(err, ErrTooManyDocuments) {
		t.Fatalf("got %v", err)
	}
}

func TestAFileThatDoesNotExistIsOpenedEmptyAndOneThatCannotBeReadIsRefused(t *testing.T) {
	r := start(t).open("fresh.go")
	if r.text() != "" || r.app.doc().changed() {
		t.Fatal("a new file is not an empty text with nothing to lose")
	}
	r.press(term.KeyF2)
	if _, made := r.files.files[root+"/fresh.go"]; !made {
		t.Fatal("saving a new file did not make it")
	}
	r.files.failing[root+"/locked.go"] = errors.New("permission denied")
	if err := r.app.Open(root + "/locked.go"); err == nil {
		t.Fatal("a file that cannot be read was opened")
	}
	r.app.open(root + "/locked.go")
	r.sees("permission denied")
	r.app.jump(Place{Path: root + "/locked.go"})
}

func TestWindowsAreSwitchedByKeyNumberAndList(t *testing.T) {
	r := start(t).open("main.go").open("notes.txt").open("shapes/shapes.go")
	r.press(term.KeyF6)
	if r.app.at != 0 {
		t.Fatalf("F6 went to %d", r.app.at)
	}
	r.chord(term.KeyF6, term.Shift)
	if r.app.at != 2 {
		t.Fatalf("Shift+F6 went to %d", r.app.at)
	}
	r.alt('2').sees(" notes.txt ")
	r.alt('9')
	if r.app.at != 1 {
		t.Fatal("a number with no window moved")
	}
	r.write("!").alt('0').sees("Windows", "1  main.go", "2  notes.txt *", "3  shapes/shapes.go")
	r.press(term.KeyDown, term.KeyDown, term.KeyEnter).sees(" shapes/shapes.go ")
	r.alt('0').press(term.KeyEsc)
}

func TestTheWindowListWithNothingOpenSaysSo(t *testing.T) {
	r := start(t)
	r.alt('0').sees("No files are open")
	r.press(term.KeyF6)
}

func TestTheMouseMovesTheCaretSelectsAndScrolls(t *testing.T) {
	r := start(t).open("main.go")
	r.send(term.Event{Kind: term.EvMouse, Button: term.MouseLeft, X: treeWidth + 3, Y: 6})
	if r.app.editor().Caret != (edit.Pos{Line: 4, Col: 2}) {
		t.Fatalf("a press put the caret at %+v", r.app.editor().Caret)
	}
	r.send(term.Event{Kind: term.EvMouse, Button: term.MouseDrag, X: treeWidth + 5, Y: 6})
	if r.app.editor().SelectedText() != "nc" {
		t.Fatalf("a drag selected %q", r.app.editor().SelectedText())
	}
	r.send(term.Event{Kind: term.EvMouse, Button: term.WheelDown, X: treeWidth + 5, Y: 6})
	if r.app.editor().ViewTop != 3 {
		t.Fatalf("the wheel scrolled to %d", r.app.editor().ViewTop)
	}
	r.sees("func main()").lacks("package main")
	r.send(term.Event{Kind: term.EvMouse, Button: term.WheelUp, X: treeWidth + 5, Y: 6})
	r.press(term.KeyRight).sees("package main")
	r.send(term.Event{Kind: term.EvMouse, Button: term.MouseLeft, X: 3, Y: 29})
}

func TestTheScreenFollowsTheTerminalsSize(t *testing.T) {
	r := start(t).open("main.go")
	r.screen()
	r.send(term.Event{Kind: term.EvResize, X: 60, Y: 12})
	if got := r.screen(); len(strings.Split(strings.TrimRight(got, "\n"), "\n")) != 12 {
		t.Fatalf("got\n%s", got)
	}
	if last := string(r.terminal.frames[len(r.terminal.frames)-1]); !strings.Contains(last, "\x1b[2J") {
		t.Fatal("a resize was not followed by a whole frame")
	}
	r.send(term.Event{Kind: term.EvResize, X: 5, Y: 3}).screen()
	r.send(term.Event{Kind: term.EvResize, X: 100, Y: 30})
	r.alt('m').send(term.Event{Kind: term.EvResize, X: 40, Y: 9}).screen()
}

func TestOnlyWhatChangedIsSentToTheTerminal(t *testing.T) {
	r := start(t).open("main.go")
	r.screen()
	r.write("x").screen()
	last := string(r.terminal.frames[len(r.terminal.frames)-1])
	if strings.Contains(last, "\x1b[2J") || len(last) > 400 {
		t.Fatalf("a keystroke sent %d bytes", len(last))
	}
}
