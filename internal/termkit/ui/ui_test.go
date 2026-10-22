package ui

import (
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/lexer"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

func key(k term.KeyCode) term.Event { return term.Event{Kind: term.EvKey, Key: k} }

func char(r rune) term.Event { return term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: r} }

// row is one row of a screen as text.
func row(s *term.Screen, y int) string {
	var sb strings.Builder
	for x := 0; x < s.W; x++ {
		sb.WriteRune(s.At(x, y).R)
	}
	return sb.String()
}

// whole is the screen as text.
func whole(s *term.Screen) string {
	var sb strings.Builder
	for y := 0; y < s.H; y++ {
		sb.WriteString(row(s, y) + "\n")
	}
	return sb.String()
}

func bar() *MenuBar {
	return &MenuBar{Menus: []Menu{
		{Title: "~F~ile", Items: []Item{
			{Label: "~N~ew", Keys: "Ctrl+N", Command: "new"},
			{},
			{Label: "E~x~it", Keys: "Alt+X", Command: "quit"},
		}},
		{Title: "~E~dit", Items: []Item{{Label: "~U~ndo", Command: "undo"}}},
	}}
}

func TestHotTextDrawsTheLetterInItsOwnStyleAndSkipsTheTildes(t *testing.T) {
	s := term.NewScreen(20, 1)
	if n := Hot(s, 0, 0, "E~x~it", Bar, BarHot); n != 4 || HotLen("E~x~it") != 4 {
		t.Fatalf("used %d cells", n)
	}
	if s.At(1, 0) != (term.Cell{R: 'x', S: BarHot}) || s.At(0, 0).S != Bar {
		t.Fatal("the hot letter is not in its style")
	}
	if HotKey("E~x~it") != 'x' || HotKey("~F~ile") != 'f' || HotKey("plain") != 0 || HotKey("odd~") != 0 {
		t.Fatal("HotKey")
	}
}

func TestTheMenuBarIsWalkedWithTheKeys(t *testing.T) {
	m := bar()
	m.Enter()
	if got := m.Key(key(term.KeyEnter)); got != "" || !m.Open {
		t.Fatal("Enter on the closed bar did not pull the menu down")
	}
	m.Key(key(term.KeyDown))
	if m.Row != 2 {
		t.Fatalf("Down did not skip the rule: row %d", m.Row)
	}
	m.Key(key(term.KeyUp))
	if m.Row != 0 {
		t.Fatalf("Up did not skip the rule: row %d", m.Row)
	}
	m.Key(key(term.KeyRight))
	if m.At != 1 || m.Row != 0 {
		t.Fatal("Right did not move to the next menu")
	}
	m.Key(key(term.KeyRight))
	m.Key(key(term.KeyLeft))
	if m.At != 1 {
		t.Fatal("the bar does not wrap round")
	}
	if got := m.Key(key(term.KeyEnter)); got != "undo" || m.Active {
		t.Fatalf("Enter chose %q", got)
	}
}

func TestAMenuIsPickedByItsLetterAndLeftWithEscape(t *testing.T) {
	m := bar()
	if !m.Pull('F') || !m.Open || m.Pull('z') {
		t.Fatal("Pull")
	}
	if got := m.Key(char('X')); got != "quit" {
		t.Fatalf("a letter chose %q", got)
	}
	m.Enter()
	m.Key(char('e'))
	if !m.Open || m.At != 1 {
		t.Fatal("a letter on the closed bar did not pull its menu")
	}
	m.Key(char('q'))
	m.Key(key(term.KeyEsc))
	if m.Open || !m.Active {
		t.Fatal("Escape did not close the menu first")
	}
	m.Key(key(term.KeyEsc))
	if m.Active || m.Key(key(term.KeyF5)) != "" {
		t.Fatal("Escape did not leave the bar")
	}
}

func TestTheMenuBarIsDrawnWithItsOpenMenu(t *testing.T) {
	s := term.NewScreen(60, 12)
	m := bar()
	m.Pull('f')
	m.Draw(s)
	if !strings.Contains(row(s, 0), "File  Edit") || !strings.Contains(row(s, 2), "New") || !strings.Contains(row(s, 2), "Ctrl+N") {
		t.Fatalf("got\n%s", whole(s))
	}
	if !strings.Contains(row(s, 3), "├") || s.At(3, 2).S != BarChosen {
		t.Fatalf("the rule or the chosen row is wrong\n%s", whole(s))
	}
}

func TestTheMenuBarAnswersTheMouse(t *testing.T) {
	s := term.NewScreen(60, 12)
	m := bar()
	if _, hit := m.Click(s, 9, 0); !hit || m.At != 1 || !m.Open {
		t.Fatal("a press on a title did not pull its menu")
	}
	if got, hit := m.Click(s, 9, 2); !hit || got != "undo" {
		t.Fatalf("a press on an item chose %q", got)
	}
	if _, hit := m.Click(s, 9, 5); hit {
		t.Fatal("a press below a closed bar was taken")
	}
	m.Pull('f')
	if got, hit := m.Click(s, 3, 3); !hit || got != "" || !m.Open {
		t.Fatal("a press on a rule chose something")
	}
	if _, hit := m.Click(s, 50, 8); !hit || m.Active {
		t.Fatal("a press outside the menu did not close it")
	}
	if _, hit := m.Click(s, 55, 0); !hit || m.Active {
		t.Fatal("a press on the empty bar")
	}
}

func TestAnInputLineIsTypedIntoAndEdited(t *testing.T) {
	in := NewInput("ab")
	in.Key(char('d'))
	in.Key(key(term.KeyLeft))
	in.Key(char('c'))
	in.Key(key(term.KeyHome))
	in.Key(key(term.KeyDelete))
	in.Key(key(term.KeyEnd))
	in.Key(key(term.KeyBackspace))
	in.Key(key(term.KeyRight))
	if in.String() != "bc" || in.Caret != 2 {
		t.Fatalf("got %q, caret %d", in.String(), in.Caret)
	}
	if in.Key(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: 'x', Mods: term.Ctrl}) || in.Key(key(term.KeyF1)) {
		t.Fatal("a key that is not typing changed the text")
	}
	in.Key(key(term.KeyHome))
	if in.Key(key(term.KeyBackspace)) {
		t.Fatal("Backspace at the start deleted something")
	}
	in.Paste("one\ntwo")
	if in.String() != "onebc" {
		t.Fatalf("a paste gave %q", in.String())
	}
}

func TestAnInputLineHoldsOnlySoMuch(t *testing.T) {
	in := NewInput("")
	in.Paste(strings.Repeat("x", MaxInput+50))
	if len(in.Text) != MaxInput {
		t.Fatalf("holds %d", len(in.Text))
	}
}

func TestAnInputLineScrollsToKeepItsCaretInView(t *testing.T) {
	s := term.NewScreen(20, 1)
	in := NewInput("abcdefghijklmnop")
	in.Draw(s, 2, 0, 8)
	if !s.CursorOn || s.CurX != 9 || s.At(2, 0).R != 'j' {
		t.Fatalf("cursor at %d, first cell %q", s.CurX, s.At(2, 0).R)
	}
}

func TestAListMovesItsChoiceAndStaysInside(t *testing.T) {
	l := List{Items: []string{"a", "b", "c", "d", "e", "f"}}
	l.Key(key(term.KeyDown), 3)
	l.Key(key(term.KeyPgDn), 3)
	if got, _ := l.Chosen(); got != "e" {
		t.Fatalf("chose %q", got)
	}
	l.Key(key(term.KeyEnd), 3)
	l.Key(key(term.KeyDown), 3)
	l.Key(key(term.KeyPgUp), 3)
	l.Key(key(term.KeyUp), 3)
	if l.Sel != 1 {
		t.Fatalf("at %d", l.Sel)
	}
	l.Key(key(term.KeyHome), 3)
	if l.Sel != 0 || l.Key(key(term.KeyEnter), 3) || l.Key(term.Event{Kind: term.EvMouse}, 3) {
		t.Fatal("Home, or a key that is not the list's")
	}
	l.Set(nil)
	if _, ok := l.Chosen(); ok {
		t.Fatal("an empty list has a choice")
	}
}

func TestAListScrollsAndAnswersAClick(t *testing.T) {
	s := term.NewScreen(20, 6)
	l := List{Items: []string{"a", "b", "c", "d", "e", "f"}, Sel: 5}
	l.Draw(s, term.Rect{X: 0, Y: 0, W: 10, H: 3}, Panel, ListChosen, true)
	if l.Top != 3 || s.At(1, 2).R != 'f' || s.At(1, 2).S != ListChosen {
		t.Fatalf("top %d\n%s", l.Top, whole(s))
	}
	if !l.Click(0) || l.Sel != 3 || l.Click(9) || l.Click(-1) {
		t.Fatal("Click")
	}
	l.Draw(s, term.Rect{W: 10}, Panel, ListChosen, true)
}

func TestAMessageBoxIsDrawnAndClosed(t *testing.T) {
	s := term.NewScreen(60, 20)
	m := NewMessage("About", []string{"Lazurite", "a console IDE for Go"})
	m.Draw(s)
	short := term.NewScreen(60, 20)
	NewMessage("Evaluate", []string{"x"}).Draw(short)
	if !strings.Contains(whole(short), " Evaluate ") {
		t.Fatalf("a box with short lines lost its title:\n%s", whole(short))
	}
	if out := whole(s); !strings.Contains(out, "About") || !strings.Contains(out, "a console IDE for Go") || !strings.Contains(out, "OK") {
		t.Fatalf("got\n%s", out)
	}
	if m.Key(key(term.KeyDown)) != Pending || m.Key(key(term.KeyEnter)) != Accepted || m.Key(key(term.KeyEsc)) != Accepted || m.Key(char(' ')) != Accepted || m.Key(char('x')) != Pending {
		t.Fatal("Key")
	}
}

func TestAQuestionHasThreeAnswers(t *testing.T) {
	s := term.NewScreen(60, 20)
	c := NewConfirm("Save", "Save changes to main.go?")
	c.Draw(s)
	if out := whole(s); !strings.Contains(out, "Save changes to main.go?") || !strings.Contains(out, "► Yes ◄") || !strings.Contains(out, "Cancel") {
		t.Fatalf("got\n%s", out)
	}
	if c.Key(key(term.KeyEnter)) != Accepted || c.Key(key(term.KeyEsc)) != Cancelled {
		t.Fatal("Enter and Escape")
	}
	c.Key(key(term.KeyRight))
	if c.Key(key(term.KeyEnter)) != Rejected {
		t.Fatal("the second button is No")
	}
	c.Key(key(term.KeyTab))
	c.Key(key(term.KeyTab))
	c.Key(key(term.KeyBackTab))
	c.Key(key(term.KeyLeft))
	if c.Key(key(term.KeyEnter)) != Rejected {
		t.Fatal("the buttons do not wrap round")
	}
	if c.Key(char('Y')) != Accepted || c.Key(char('n')) != Rejected || c.Key(char('c')) != Cancelled || c.Key(char('q')) != Pending || c.Key(key(term.KeyF1)) != Pending {
		t.Fatal("letters")
	}
}

func TestAPromptTakesOneLine(t *testing.T) {
	s := term.NewScreen(70, 20)
	p := NewPrompt("Go to line", "Line number", "1")
	p.Key(char('2'))
	p.Key(term.Event{Kind: term.EvPaste, Text: "3\n4"})
	p.Draw(s)
	if p.Input.String() != "123" || !strings.Contains(whole(s), "Line number") || !s.CursorOn {
		t.Fatalf("got %q", p.Input.String())
	}
	if p.Key(key(term.KeyEnter)) != Accepted || p.Key(key(term.KeyEsc)) != Cancelled {
		t.Fatal("Enter and Escape")
	}
}

func TestAPickerNarrowsToWhatIsTyped(t *testing.T) {
	s := term.NewScreen(80, 24)
	p := NewPicker("Open", []string{"../", "cmd/", "main.go", "main_test.go", "README.md"})
	p.Key(char('m'))
	p.Key(char('a'))
	p.Key(key(term.KeyDown))
	p.Draw(s)
	if p.Choice() != "main_test.go" || strings.Contains(whole(s), "README") {
		t.Fatalf("chose %q\n%s", p.Choice(), whole(s))
	}
	if p.Key(key(term.KeyEnter)) != Accepted {
		t.Fatal("Enter did not accept a match")
	}
	p.Key(term.Event{Kind: term.EvPaste, Text: "zzz"})
	if p.Key(key(term.KeyEnter)) != Pending {
		t.Fatal("Enter accepted with nothing matching")
	}
	p.Free = true
	if p.Key(key(term.KeyEnter)) != Accepted || p.Choice() != "mazzz" {
		t.Fatalf("a free picker gave %q", p.Choice())
	}
	p.Key(key(term.KeyHome))
	if p.Input.Caret != 0 {
		t.Fatal("Home did not go to the line being typed")
	}
	p.Reset("Open cmd", []string{"lazurite/"})
	if p.Choice() != "lazurite/" || p.Input.String() != "" || p.Key(key(term.KeyEsc)) != Cancelled {
		t.Fatal("Reset")
	}
}

func TestMatchingWantsEveryWordInAnyCase(t *testing.T) {
	lines := []string{"internal/core/Edit/buffer.go", "internal/core/term/input.go", "cmd/main.go"}
	if got := Matching(lines, "CORE .go edit"); len(got) != 1 || got[0] != lines[0] {
		t.Fatalf("got %v", got)
	}
	if len(Matching(lines, "  ")) != 3 || len(Matching(lines, "absent")) != 0 {
		t.Fatal("Matching")
	}
}

func TestAWindowIsFramedTitledAndNumbered(t *testing.T) {
	s := term.NewScreen(40, 8)
	Window(s, term.Rect{W: 40, H: 8}, Title{Name: "main.go", Number: 2, Foot: "12:5", Changed: true, Active: true}, Text)
	if !strings.Contains(row(s, 0), "[■]") || !strings.Contains(row(s, 0), " main.go * ") || !strings.Contains(row(s, 0), "2") {
		t.Fatalf("got %q", row(s, 0))
	}
	if !strings.HasPrefix(row(s, 7), "╚═ 12:5 ═") || s.At(0, 3).R != '║' || s.At(5, 3).S != Text {
		t.Fatalf("got %q", row(s, 7))
	}
	Window(s, term.Rect{W: 3, H: 1}, Title{}, Text)
	idle := term.NewScreen(40, 8)
	Window(idle, term.Rect{W: 40, H: 8}, Title{Name: "x"}, Text)
	if strings.Contains(row(idle, 0), "■") || idle.At(0, 0).S.Fg != FrameIdle.Fg {
		t.Fatal("an idle window is drawn as the active one")
	}
}

func TestClipKeepsTheEndOfAPath(t *testing.T) {
	if Clip("internal/core/edit/buffer.go", 12) != "…t/buffer.go" || Clip("short", 12) != "short" || Clip("abc", 1) != "a" || Clip("abc", 0) != "" {
		t.Fatalf("got %q", Clip("internal/core/edit/buffer.go", 12))
	}
}

func TestAShadowDarkensWhatIsUnderItAndKeepsTheCharacter(t *testing.T) {
	s := term.NewScreen(12, 6)
	s.Fill(term.Rect{W: 12, H: 6}, 'x', Text)
	Shadow(s, term.Rect{X: 1, Y: 1, W: 4, H: 2})
	if s.At(5, 2) != (term.Cell{R: 'x', S: Shade}) || s.At(3, 3).S != Shade || s.At(1, 1).S != Text {
		t.Fatal("the shadow is in the wrong place")
	}
	Shadow(s, term.Rect{X: 9, Y: 4, W: 4, H: 4})
}

func TestTheStatusLineNamesKeysAndCarriesAMessage(t *testing.T) {
	s := term.NewScreen(60, 3)
	Status(s, []Hint{{"F2", "Save"}, {"F3", "Open"}}, "Saved main.go")
	if got := row(s, 2); !strings.HasPrefix(got, " F2 Save  F3 Open") || !strings.HasSuffix(got, "Saved main.go ") {
		t.Fatalf("got %q", got)
	}
	if s.At(1, 2).S != BarHot || s.At(4, 2).S != Bar {
		t.Fatal("the key is not highlighted")
	}
	narrow := term.NewScreen(20, 1)
	Status(narrow, []Hint{{"F2", "Save"}, {"F3", "Open"}, {"F10", "Menu"}}, "a message far too long to fit")
	if got := row(narrow, 0); !strings.Contains(got, "…") || strings.Contains(got, "F2") {
		t.Fatalf("a message too long for the line was not cut, or a key was drawn over it: %q", got)
	}
	Status(narrow, []Hint{{"F2", "Save"}, {"F3", "Open"}}, "Saved")
	if got := row(narrow, 0); !strings.HasPrefix(got, " F2 Save") || strings.Contains(got, "F3") || !strings.HasSuffix(got, "Saved ") {
		t.Fatalf("the keys that fit beside the message were not the ones drawn: %q", got)
	}
}

func TestCenteredFitsTheScreen(t *testing.T) {
	s := term.NewScreen(40, 10)
	if r := Centered(s, 20, 4); r != (term.Rect{X: 10, Y: 3, W: 20, H: 4}) {
		t.Fatalf("got %+v", r)
	}
	if r := Centered(s, 200, 200); r.W != 36 || r.H != 8 {
		t.Fatalf("got %+v", r)
	}
}

func goPicture(text string) Picture {
	lang, _ := lexer.For("x.go")
	return Picture{Spans: ByLine(lexer.Scan(lang, text), strings.Count(text, "\n")+1), Resume: -1}
}

func TestTheEditorIsDrawnInItsColours(t *testing.T) {
	text := "package main\n\n// żółw\nfunc main() {\n\tx := \"hi\" + 42\n}"
	s := term.NewScreen(40, 8)
	ed := edit.NewEditor(text)
	DrawEditor(s, term.Rect{X: 1, Y: 1, W: 30, H: 6}, ed, goPicture(text))
	if s.At(1, 1).S != Keyword || s.At(9, 1).S == Keyword {
		t.Fatal("`package` is not a keyword, or `main` is")
	}
	if s.At(4, 3).R != 'ż' || s.At(4, 3).S != Comment || s.At(7, 3).S != Comment {
		t.Fatal("a comment with accents lost its colour")
	}
	if s.At(1, 5).R != ' ' || s.At(5, 5).R != 'x' || s.At(10, 5).S != Literal || s.At(17, 5).S != Number {
		t.Fatalf("a tab was not opened out, or a literal lost its colour\n%s", whole(s))
	}
	if !s.CursorOn || s.CurX != 1 || s.CurY != 1 {
		t.Fatal("the cursor is not on the caret")
	}
}

func TestTheEditorShowsSelectionFaultsBreakpointsAndTheResumeLine(t *testing.T) {
	text := "one\ntwo\nthree\nfour"
	s := term.NewScreen(20, 6)
	ed := edit.NewEditor(text)
	ed.Select(edit.Pos{Line: 0, Col: 1}, edit.Pos{Line: 0, Col: 3})
	p := Picture{Resume: 3, Breaks: map[int]bool{2: true}, Marks: []Mark{
		{From: edit.Pos{Line: 1, Col: 0}, To: edit.Pos{Line: 1, Col: 2}, Severe: true},
		{From: edit.Pos{Line: 1, Col: 2}, To: edit.Pos{Line: 1, Col: 3}},
	}}
	DrawEditor(s, term.Rect{W: 20, H: 6}, ed, p)
	if s.At(0, 0).S != Text || s.At(1, 0).S != Selection || s.At(3, 0).S == Selection {
		t.Fatal("the selection")
	}
	if s.At(0, 1).S != Fault || s.At(2, 1).S != Doubt {
		t.Fatal("the marks")
	}
	if s.At(0, 2).S != BreakLine || s.At(15, 2).S != BreakLine || s.At(2, 3).S != ResumeLine {
		t.Fatal("the breakpoint or the resume line")
	}
}

func TestTheEditorScrollsSidewaysAndFindsThePositionUnderACell(t *testing.T) {
	text := "\tabcdefghijklmnopqrstuvwxyz\nshort"
	s := term.NewScreen(12, 4)
	ed := edit.NewEditor(text)
	ed.End(false)
	r := term.Rect{X: 1, Y: 1, W: 10, H: 2}
	DrawEditor(s, r, ed, Picture{Resume: -1})
	if ed.ViewLeft != 21 || s.At(1, 1).R != 'r' || s.CurX != 10 {
		t.Fatalf("left %d, first cell %q, cursor %d", ed.ViewLeft, s.At(1, 1).R, s.CurX)
	}
	if s.At(0, 1).R != ' ' || s.At(11, 1).R != ' ' {
		t.Fatal("the text was drawn outside its rectangle")
	}
	if got := PosAt(ed, r, 2, 1); got != (edit.Pos{Line: 0, Col: 19}) {
		t.Fatalf("got %+v", got)
	}
	if got := PosAt(ed, r, 3, 9); got.Line != 1 {
		t.Fatalf("a cell below the text gave %+v", got)
	}
}

func BenchmarkDrawEditor(b *testing.B) {
	text := strings.Repeat("\tif err != nil { return fmt.Errorf(\"x: %w\", err) } // comment\n", 200)
	s := term.NewScreen(120, 40)
	ed := edit.NewEditor(text)
	p := goPicture(text)
	for b.Loop() {
		DrawEditor(s, term.Rect{W: 120, H: 40}, ed, p)
	}
}

func BenchmarkMatching(b *testing.B) {
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = "internal/core/package/file.go"
	}
	for b.Loop() {
		Matching(lines, "core file")
	}
}

func BenchmarkHot(b *testing.B) {
	s := term.NewScreen(40, 1)
	for b.Loop() {
		Hot(s, 0, 0, "~F~ile", Bar, BarHot)
	}
}

func BenchmarkClip(b *testing.B) {
	for b.Loop() {
		Clip("internal/core/edit/buffer.go", 12)
	}
}

func TestAPickerCanBeGivenASearchOfItsOwn(t *testing.T) {
	p := NewPicker("Targets", []string{"one", "two"})
	p.Match = func(query string) []string { return []string{"matched " + query} }
	p.Key(char('x'))
	if p.Choice() != "matched x" {
		t.Fatalf("got %q", p.Choice())
	}
}
