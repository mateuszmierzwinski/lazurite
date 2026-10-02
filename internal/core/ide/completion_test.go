package ide

import (
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/core/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
)

func fmtOffers() []Suggestion {
	return []Suggestion{
		{Label: "Print", Detail: "func(a ...any)", Insert: "Print"},
		{Label: "Printf", Detail: "func(format string, a ...any)", Insert: "Printf"},
		{Label: "Println", Detail: "func(a ...any)", Insert: "Println"},
		{Label: "Sprint", Insert: "Sprint"},
	}
}

func TestADotOffersCompletionsAndTypingNarrowsThem(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.offers = fmtOffers()
	r.caret(5, 19).press(term.KeyEnter).write("fmt.").sees("Print  func(a ...any)", "Sprint")
	r.write("Pr").sees("Printf", "Println").lacks("Sprint")
	if len(r.intel.asked) != 2 {
		t.Fatalf("asked %d times: once for `fmt`, once at the dot — never per letter", len(r.intel.asked))
	}
	r.press(term.KeyDown, term.KeyEnter)
	if !strings.Contains(r.text(), "\tfmt.Printf\n") || r.app.popup != nil {
		t.Fatalf("got %q", r.text())
	}
}

func TestACompletionIsTakenWithTabAndDismissedWithEscape(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.offers = fmtOffers()
	r.caret(5, 19).press(term.KeyEnter).write("fmt.S").press(term.KeyTab)
	if !strings.Contains(r.text(), "fmt.Sprint\n") {
		t.Fatalf("got %q", r.text())
	}
	r.write(".P").sees("Println").press(term.KeyEsc).lacks("Println  func")
	if strings.Contains(r.text(), "Println\n") {
		t.Fatal("Escape took a completion")
	}
}

func TestBackspaceWidensTheListAndLeavingTheWordClosesIt(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.offers = fmtOffers()
	r.caret(5, 19).press(term.KeyEnter).write("fmt.Printl").lacks("Printf  func")
	r.press(term.KeyBackspace, term.KeyBackspace).sees("Printf  func", "Println  func")
	r.press(term.KeyLeft, term.KeyLeft, term.KeyLeft, term.KeyLeft, term.KeyLeft, term.KeyLeft)
	if r.app.popup != nil {
		t.Fatal("the list stayed open after the caret left the word")
	}
	r.write("x.Pr z").press(term.KeyPgDn)
	if r.app.popup != nil {
		t.Fatal("a space did not close the list")
	}
}

func TestNothingMatchingClosesTheListAndCtrlSpaceAsksAgain(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.offers = fmtOffers()
	r.caret(5, 19).press(term.KeyEnter).write("fmt.Q")
	if r.app.popup != nil {
		t.Fatal("a list with nothing matching stayed open")
	}
	r.press(term.KeyBackspace).ctrl(' ').sees("Println  func")
	r.press(term.KeyPgDn, term.KeyPgUp, term.KeyF5)
	if r.app.popup != nil {
		t.Fatal("a command left the list open")
	}
}

func TestAnAnswerThatArrivesLateIsDropped(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.hold = true
	r.caret(5, 19).press(term.KeyEnter).write("fmt.")
	late := r.intel.holdOffer
	r.press(term.KeyUp)
	late(fmtOffers())
	r.app.Drain()
	if r.app.popup != nil {
		t.Fatal("completions for another place were offered")
	}
	r.press(term.KeyDown).ctrl(' ')
	r.press(term.KeyF10)
	r.intel.holdOffer(fmtOffers())
	r.app.Drain()
	if r.app.popup != nil {
		t.Fatal("completions were offered over the menu")
	}
}

func TestMoreCompletionsThanCanBeShownAreCut(t *testing.T) {
	r := start(t).open("main.go")
	for i := 0; i < maxSuggestions+50; i++ {
		r.intel.offers = append(r.intel.offers, Suggestion{Label: "Name", Insert: "Name"})
	}
	r.caret(5, 19).press(term.KeyEnter).write("x.")
	if len(r.app.popup.all) != maxSuggestions {
		t.Fatalf("holds %d", len(r.app.popup.all))
	}
}

func TestACompletionAddsTheImportItNeedsAndTheCaretStaysOnItsLine(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.offers = []Suggestion{{
		Label: "ToUpper", Insert: "ToUpper",
		Extra: []Edit{{From: edit.Pos{Line: 2, Col: 0}, To: edit.Pos{Line: 2, Col: 12}, Text: "import (\n\t\"fmt\"\n\t\"strings\"\n)"}},
	}}
	r.caret(5, 19).press(term.KeyEnter).write("strings.To").press(term.KeyEnter)
	want := "import (\n\t\"fmt\"\n\t\"strings\"\n)\n\nfunc main() {\n\tfmt.Println(\"hi\")\n\tstrings.ToUpper\n}"
	if !strings.Contains(r.text(), want) {
		t.Fatalf("got %q", r.text())
	}
	if got := r.app.editor().Caret; got != (edit.Pos{Line: 9, Col: 16}) {
		t.Fatalf("the caret is at %+v", got)
	}
}

func TestTheListIsDrawnAboveTheCaretWhenThereIsNoRoomBelow(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.offers = fmtOffers()
	r.send(term.Event{Kind: term.EvResize, X: 60, Y: 12})
	r.caret(6, 1)
	for i := 0; i < 6; i++ {
		r.press(term.KeyEnter)
	}
	r.write("fmt.").sees("Println")
	if y := r.app.screen.CurY; y < 5 {
		t.Fatalf("the caret is on row %d", y)
	}
}

func TestATextThatHasNoFileYetIsCompletedToo(t *testing.T) {
	r := start(t)
	r.intel.offers = fmtOffers()
	r.ctrl('n').write("fmt.Pr").sees("Printf", "Println")
	if got := r.intel.log; len(got) == 0 || got[0] != "open noname01.go" {
		t.Fatalf("the language server was told %v — a new text has to be given a place to be completed in", got)
	}
	if r.app.doc().path != root+"/noname01.go" || !r.app.doc().unnamed {
		t.Fatalf("the new text lives at %q", r.app.doc().path)
	}
	r.press(term.KeyEsc, term.KeyF2).sees("Save as")
}

func TestANewTextNeverTakesTheNameOfAFileThatExists(t *testing.T) {
	r := start(t)
	r.files.files[root+"/noname01.go"] = "package taken\n"
	r.ctrl('n').ctrl('n').sees("noname03.go")
	if r.app.docs[0].path != root+"/noname02.go" {
		t.Fatalf("the first new text lives at %q", r.app.docs[0].path)
	}
}

func TestAskingForCompletionsWhereThereAreNoneSaysSo(t *testing.T) {
	r := start(t).open("main.go")
	r.ctrl(' ').sees("No completions here")
	r.caret(5, 19).press(term.KeyEnter).write("x")
	r.lacks("No completions here")
}

func BenchmarkBeginning(b *testing.B) {
	all := fmtOffers()
	for b.Loop() {
		beginning(all, "pr")
	}
}

func BenchmarkLatestFirst(b *testing.B) {
	edits := []Edit{{From: edit.Pos{Line: 1}}, {From: edit.Pos{Line: 9}}, {From: edit.Pos{Line: 4}}}
	for b.Loop() {
		latestFirst(edits)
	}
}
