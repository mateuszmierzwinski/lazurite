package edit

import (
	"strings"
	"testing"
)

func TestABufferReadsAnySystemsLineEndingsAndAlwaysHasALine(t *testing.T) {
	b := NewBuffer("a\r\nb\rc\nd")
	if b.Lines() != 4 || b.Text() != "a\nb\nc\nd" {
		t.Fatalf("got %d lines: %q", b.Lines(), b.Text())
	}
	if e := NewBuffer(""); e.Lines() != 1 || e.Text() != "" || e.End() != (Pos{}) {
		t.Fatal("an empty text is not one empty line")
	}
	if b.Line(-1) != nil || b.Line(99) != nil {
		t.Fatal("a line that does not exist is not empty")
	}
}

func TestReplaceWithinALineAcrossLinesAndWithNewLines(t *testing.T) {
	b := NewBuffer("hello world\nsecond\nthird")
	if end := b.Replace(Pos{0, 6}, Pos{0, 11}, "there"); end != (Pos{0, 11}) || b.Line(0)[6] != 't' {
		t.Fatalf("end %+v, %q", end, b.Text())
	}
	if end := b.Replace(Pos{0, 5}, Pos{2, 2}, ","); end != (Pos{0, 6}) || b.Text() != "hello,ird" {
		t.Fatalf("end %+v, %q", end, b.Text())
	}
	if end := b.Replace(Pos{0, 5}, Pos{0, 6}, "\none\ntwo "); end != (Pos{2, 4}) || b.Text() != "hello\none\ntwo ird" {
		t.Fatalf("end %+v, %q", end, b.Text())
	}
	if end := b.Replace(Pos{9, 9}, Pos{0, 0}, ""); end != (Pos{}) || b.Text() != "" {
		t.Fatalf("positions out of range and out of order: %+v %q", end, b.Text())
	}
}

func TestRangeIsTheTextBetweenTwoPositionsInEitherOrder(t *testing.T) {
	b := NewBuffer("abc\ndef\nghi")
	if b.Range(Pos{0, 1}, Pos{2, 2}) != "bc\ndef\ngh" || b.Range(Pos{2, 2}, Pos{0, 1}) != "bc\ndef\ngh" || b.Range(Pos{1, 1}, Pos{1, 2}) != "e" {
		t.Fatal("Range")
	}
}

func TestUndoAndRedoWalkTheStepsAndTypingIsOneStep(t *testing.T) {
	b := NewBuffer("")
	p := Pos{}
	for _, r := range "func" {
		p = b.Type(p, r)
	}
	b.Seal()
	p = b.Type(p, ' ')
	b.Replace(Pos{0, 0}, Pos{0, 4}, "var")
	if b.Text() != "var " {
		t.Fatalf("got %q", b.Text())
	}
	for _, want := range []string{"func ", "func", ""} {
		if _, ok := b.Undo(); !ok || b.Text() != want {
			t.Fatalf("undo gave %q, want %q", b.Text(), want)
		}
	}
	if _, ok := b.Undo(); ok {
		t.Fatal("undo past the beginning")
	}
	for _, want := range []string{"func", "func ", "var "} {
		if _, ok := b.Redo(); !ok || b.Text() != want {
			t.Fatalf("redo gave %q, want %q", b.Text(), want)
		}
	}
	if _, ok := b.Redo(); ok {
		t.Fatal("redo past the end")
	}
	b.Undo()
	b.Type(Pos{0, 5}, 'x')
	if _, ok := b.Redo(); ok {
		t.Fatal("a new edit did not forget what could be redone")
	}
}

func TestANewLineOrAJumpEndsARunOfTyping(t *testing.T) {
	b := NewBuffer("")
	p := b.Type(Pos{}, 'a')
	p = b.Type(p, '\n')
	b.Type(p, 'b')
	b.Type(Pos{0, 0}, 'z')
	steps := 0
	for {
		if _, ok := b.Undo(); !ok {
			break
		}
		steps++
	}
	if steps != 4 || b.Text() != "" {
		t.Fatalf("%d steps back to %q", steps, b.Text())
	}
}

func TestUndoRemembersOnlySoManySteps(t *testing.T) {
	b := NewBuffer("")
	for i := 0; i < MaxUndo+50; i++ {
		b.Replace(Pos{}, Pos{}, "x")
	}
	steps := 0
	for {
		if _, ok := b.Undo(); !ok {
			break
		}
		steps++
	}
	if steps != MaxUndo {
		t.Fatalf("%d steps were remembered", steps)
	}
}

func TestSetTextIsOneStepAndTheSameTextIsNoStep(t *testing.T) {
	b := NewBuffer("one\ntwo")
	b.SetText("one\ntwo")
	if _, ok := b.Undo(); ok {
		t.Fatal("setting the same text recorded a step")
	}
	b.SetText("three")
	if _, ok := b.Undo(); !ok || b.Text() != "one\ntwo" {
		t.Fatalf("got %q", b.Text())
	}
}

func TestTheCaretMovesByRunesLinesAndWords(t *testing.T) {
	e := NewEditor("ab\n\tcd efg\nx")
	e.Right(false)
	e.Right(false)
	e.Right(false)
	if e.Caret != (Pos{1, 0}) {
		t.Fatalf("right past the end of a line: %+v", e.Caret)
	}
	e.Left(false)
	if e.Caret != (Pos{0, 2}) {
		t.Fatalf("left past the start of a line: %+v", e.Caret)
	}
	e.Goto(Pos{1, 1})
	e.WordRight(false)
	e.WordRight(false)
	if e.Caret != (Pos{1, 7}) {
		t.Fatalf("two words right: %+v", e.Caret)
	}
	e.WordLeft(false)
	if e.Caret != (Pos{1, 4}) {
		t.Fatalf("a word left: %+v", e.Caret)
	}
	e.DocEnd(false)
	e.DocStart(true)
	if e.SelectedText() != "ab\n\tcd efg\nx" {
		t.Fatalf("selected %q", e.SelectedText())
	}
	e.Left(false)
	e.WordLeft(false)
	e.DocEnd(false)
	e.WordRight(false)
	if e.Caret != e.Buf.End() {
		t.Fatal("word motion at the ends of the text")
	}
}

func TestMovingUpAndDownKeepsToTheColumnItStartedFrom(t *testing.T) {
	e := NewEditor("long line here\nab\n\tlonger again")
	e.Goto(Pos{0, 10})
	e.Vertical(1, false)
	if e.Caret != (Pos{1, 2}) {
		t.Fatalf("down onto a short line: %+v", e.Caret)
	}
	e.Vertical(1, false)
	if e.Column() != 10 {
		t.Fatalf("down again is at visual column %d, want 10", e.Column())
	}
	e.Vertical(-99, false)
	if e.Caret.Line != 0 {
		t.Fatal("up past the top")
	}
}

func TestHomeGoesToTheFirstNonBlankThenToTheStart(t *testing.T) {
	e := NewEditor("\t\tx := 1")
	e.End(false)
	e.Home(false)
	if e.Caret.Col != 2 {
		t.Fatalf("home: %+v", e.Caret)
	}
	e.Home(false)
	if e.Caret.Col != 0 {
		t.Fatalf("home again: %+v", e.Caret)
	}
}

func TestVisualColumnsCountATabToTheNextStop(t *testing.T) {
	line := []rune("\ta\tb")
	for col, want := range map[int]int{0: 0, 1: 4, 2: 5, 3: 8, 4: 9} {
		if got := Visual(line, col); got != want {
			t.Errorf("Visual(%d) = %d, want %d", col, got, want)
		}
	}
	for visual, want := range map[int]int{0: 0, 1: 0, 3: 1, 4: 1, 5: 2, 8: 3, 99: 4} {
		if got := ColAt(line, visual); got != want {
			t.Errorf("ColAt(%d) = %d, want %d", visual, got, want)
		}
	}
}

func TestTypingReplacesTheSelectionAndEnterIndents(t *testing.T) {
	e := NewEditor("\tif x {")
	e.End(false)
	e.Enter()
	e.Type('y')
	if e.Buf.Text() != "\tif x {\n\t\ty" {
		t.Fatalf("got %q", e.Buf.Text())
	}
	e.Enter()
	if e.Buf.Text() != "\tif x {\n\t\ty\n\t\t" {
		t.Fatalf("got %q", e.Buf.Text())
	}
	e.Select(Pos{0, 1}, Pos{0, 3})
	e.Type('F')
	if e.Buf.Line(0)[1] != 'F' || len(e.Buf.Line(0)) != 6 || e.Selected() {
		t.Fatalf("got %q", string(e.Buf.Line(0)))
	}
}

func TestBackspaceAndDeleteJoinLinesAndRemoveTheSelection(t *testing.T) {
	e := NewEditor("ab\ncd")
	e.Goto(Pos{1, 0})
	e.Backspace()
	if e.Buf.Text() != "abcd" || e.Caret != (Pos{0, 2}) {
		t.Fatalf("backspace at the start of a line: %q %+v", e.Buf.Text(), e.Caret)
	}
	e.Delete()
	if e.Buf.Text() != "abd" {
		t.Fatalf("delete: %q", e.Buf.Text())
	}
	e.SelectAll()
	e.Backspace()
	e.Backspace()
	e.Delete()
	if e.Buf.Text() != "" {
		t.Fatalf("got %q", e.Buf.Text())
	}
}

func TestCutPasteUndoRedo(t *testing.T) {
	e := NewEditor("one two")
	e.Select(Pos{0, 0}, Pos{0, 4})
	if got := e.Cut(); got != "one " || e.Buf.Text() != "two" {
		t.Fatalf("cut %q, left %q", got, e.Buf.Text())
	}
	e.End(false)
	e.Insert(" one\nmore")
	if e.Buf.Text() != "two one\nmore" || e.Caret != (Pos{1, 4}) {
		t.Fatalf("paste: %q %+v", e.Buf.Text(), e.Caret)
	}
	e.Undo()
	e.Undo()
	if e.Buf.Text() != "one two" {
		t.Fatalf("two undos: %q", e.Buf.Text())
	}
	e.Redo()
	if e.Buf.Text() != "two" || e.Cut() != "" {
		t.Fatalf("redo: %q", e.Buf.Text())
	}
	e.Undo()
	e.Undo()
	e.Redo()
	e.Redo()
	e.Redo()
}

func TestRevealScrollsTheCaretIntoViewBothWays(t *testing.T) {
	e := NewEditor(strings.Repeat("x\n", 100) + strings.Repeat("y", 200))
	e.Goto(Pos{60, 0})
	e.Reveal(40, 20)
	if e.ViewTop != 41 {
		t.Fatalf("top %d", e.ViewTop)
	}
	e.DocEnd(false)
	e.Reveal(40, 20)
	if e.ViewLeft != 161 || e.ViewTop != 81 {
		t.Fatalf("left %d top %d", e.ViewLeft, e.ViewTop)
	}
	e.DocStart(false)
	e.Reveal(40, 20)
	e.Reveal(0, 0)
	if e.ViewLeft != 0 || e.ViewTop != 0 {
		t.Fatalf("left %d top %d", e.ViewLeft, e.ViewTop)
	}
	e.Scroll(500)
	if e.ViewTop != 100 {
		t.Fatalf("scrolled to %d", e.ViewTop)
	}
	e.Scroll(-999)
	if e.ViewTop != 0 {
		t.Fatal("scrolled above the top")
	}
}

func TestFindSelectsTheNextOccurrenceAndWrapsRound(t *testing.T) {
	e := NewEditor("go\nlang go\nę go")
	for _, want := range []Pos{{0, 0}, {1, 5}, {2, 2}, {0, 0}} {
		if !e.Find("go") {
			t.Fatal("not found")
		}
		if from, _ := e.Selection(); from != want {
			t.Fatalf("found at %+v, want %+v", from, want)
		}
	}
	if e.Find("absent") || e.Find("") {
		t.Fatal("found what is not there")
	}
}

func TestWordIsWhatIsBeingTyped(t *testing.T) {
	e := NewEditor("fmt.Printl x")
	e.Goto(Pos{0, 10})
	if w, at := e.Word(); w != "Printl" || at != (Pos{0, 4}) {
		t.Fatalf("got %q at %+v", w, at)
	}
	e.Goto(Pos{0, 4})
	if w, _ := e.Word(); w != "" {
		t.Fatalf("after a dot the word is %q", w)
	}
}

func BenchmarkReplace(b *testing.B) {
	buf := NewBuffer(strings.Repeat("some line of text\n", 2000))
	for b.Loop() {
		buf.Replace(Pos{1000, 3}, Pos{1000, 3}, "x")
		buf.Replace(Pos{1000, 3}, Pos{1000, 4}, "")
	}
}

func BenchmarkText(b *testing.B) {
	buf := NewBuffer(strings.Repeat("some line of text\n", 2000))
	for b.Loop() {
		_ = buf.Text()
	}
}

func BenchmarkVisual(b *testing.B) {
	line := []rune("\t\tif err != nil {\treturn err }")
	for b.Loop() {
		Visual(line, len(line))
	}
}

func BenchmarkColAt(b *testing.B) {
	line := []rune("\t\tif err != nil {\treturn err }")
	for b.Loop() {
		ColAt(line, 20)
	}
}

func BenchmarkFind(b *testing.B) {
	e := NewEditor(strings.Repeat("some line of text\n", 500) + "needle")
	for b.Loop() {
		e.Goto(Pos{})
		e.Find("needle")
	}
}
