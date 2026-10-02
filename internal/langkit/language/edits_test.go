package language

import (
	"errors"
	"strings"
	"testing"
)

// at builds a position.
func at(line, character int) Position { return Position{Line: line, Character: character} }

// span builds a range between two points.
func span(fromLine, fromCol, toLine, toCol int) Range {
	return Range{Start: at(fromLine, fromCol), End: at(toLine, toCol)}
}

// edit builds one replacement.
func edit(r Range, text string) TextEdit { return TextEdit{Span: r, NewText: text} }

func TestEditsAreAppliedAgainstTheOriginalText(t *testing.T) {
	// The protocol's rule, and the one that makes a rename correct: every
	// range is against the text as it was, so applying them in order would send
	// the second edit's offsets into a document the first one has moved.
	before := "alpha beta gamma\n"
	after, err := Apply(before, []TextEdit{
		edit(span(0, 0, 0, 5), "one"),
		edit(span(0, 11, 0, 16), "three"),
	})
	if err != nil {
		t.Fatalf("Apply = %v", err)
	}
	if after != "one beta three\n" {
		t.Fatalf("after = %q", after)
	}
}

func TestAnEditOnASecondLineLandsOnThatLine(t *testing.T) {
	after, err := Apply("one\ntwo\nthree\n", []TextEdit{edit(span(1, 0, 1, 3), "TWO")})
	if err != nil || after != "one\nTWO\nthree\n" {
		t.Fatalf("Apply = %q, %v", after, err)
	}
}

func TestAPositionIsCountedInUTF16CodeUnits(t *testing.T) {
	// The conversion the protocol forces on every client. An astral rune is
	// TWO code units and one rune and four bytes, so a client counting the
	// wrong one puts an edit a character or two off — in exactly the files with
	// an emoji or an accent in them.
	after, err := Apply("// 😀 x\n", []TextEdit{edit(span(0, 6, 0, 7), "y")})
	if err != nil {
		t.Fatalf("Apply = %v", err)
	}
	if after != "// 😀 y\n" {
		t.Fatalf("after = %q, want the x replaced and the emoji intact", after)
	}
}

func TestAnAccentedLineIsCountedCorrectly(t *testing.T) {
	// A rune below U+10000 is one code unit and two bytes: the trap in the
	// other direction.
	after, err := Apply("é = 1\n", []TextEdit{edit(span(0, 4, 0, 5), "2")})
	if err != nil || after != "é = 2\n" {
		t.Fatalf("Apply = %q, %v", after, err)
	}
}

func TestOverlappingEditsAreRefusedRatherThanSpliced(t *testing.T) {
	// Two edits over the same text is a server fault, and splicing them anyway
	// produces a file nobody wrote.
	_, err := Apply("alpha beta\n", []TextEdit{
		edit(span(0, 0, 0, 5), "one"),
		edit(span(0, 3, 0, 8), "two"),
	})
	if !errors.Is(err, ErrOverlappingEdits) {
		t.Fatalf("Apply = %v, want ErrOverlappingEdits", err)
	}
}

func TestAnEditOutsideTheDocumentIsRefusedRatherThanClamped(t *testing.T) {
	// Clamping would apply the change to the wrong place and report success,
	// which is worse than not applying it: the operator has no reason to look.
	for name, e := range map[string]TextEdit{
		"a line past the end": edit(span(9, 0, 9, 1), "x"),
		"a negative line":     edit(span(-1, 0, 0, 1), "x"),
		"an inverted range":   edit(span(0, 4, 0, 1), "x"),
	} {
		if _, err := Apply("one\ntwo\n", []TextEdit{e}); !errors.Is(err, ErrEditOutOfRange) {
			t.Fatalf("%s: Apply = %v, want ErrEditOutOfRange", name, err)
		}
	}
}

func TestAColumnPastTheEndOfALineMeansItsEnd(t *testing.T) {
	// The protocol's own instruction: servers routinely name a column beyond
	// the last character to mean "the end of this line".
	after, err := Apply("one\ntwo\n", []TextEdit{edit(span(0, 3, 0, 40), "!")})
	if err != nil || after != "one!\ntwo\n" {
		t.Fatalf("Apply = %q, %v", after, err)
	}
}

func TestAnEmptyEditSetLeavesTheTextAlone(t *testing.T) {
	after, err := Apply("one\n", nil)
	if err != nil || after != "one\n" {
		t.Fatalf("Apply = %q, %v", after, err)
	}
}

func TestAPositionIsFoundFromARuneOffset(t *testing.T) {
	text := "one\ntwö\nthree"
	for name, c := range map[string]struct {
		offset int
		want   Position
	}{
		"the start":        {0, at(0, 0)},
		"mid first line":   {2, at(0, 2)},
		"the second line":  {4, at(1, 0)},
		"after an accent":  {7, at(1, 3)},
		"past the end":     {999, at(2, 5)},
		"a negative asked": {-1, at(0, 0)},
	} {
		if got := PositionOf(text, c.offset); got != c.want {
			t.Fatalf("%s: PositionOf = %+v, want %+v", name, got, c.want)
		}
	}
}

func TestSupportsReadsWhatTheServerAdvertised(t *testing.T) {
	caps := Capabilities{Hover: true, CodeAction: true}
	if !Supports(caps, FeatureHover) {
		t.Fatal("a server advertising hover was reported as not doing it")
	}
	if Supports(caps, FeatureRename) {
		t.Fatal("a server that never mentioned rename was reported as doing it")
	}
	if !Supports(caps, FeatureOrganiseImports) {
		t.Fatal("organise imports is a code action and must follow that capability")
	}
	if Supports(Capabilities{}, FeatureDefinition) {
		t.Fatal("the zero capability set supported something")
	}
}

func TestAnUnknownFeatureIsUnsupported(t *testing.T) {
	// The safe direction: a feature nobody has mapped is one nobody has
	// implemented, and offering it would be a menu entry that answers with a
	// protocol error.
	if Supports(Capabilities{Hover: true}, Feature(99)) {
		t.Fatal("an unmapped feature was reported as supported")
	}
}

func TestEveryFeatureIsNamedForAReader(t *testing.T) {
	// The name reaches the status bar, so it must be prose rather than a method
	// name — and every feature must have one, or a failure reads as "unknown".
	for f := FeatureDefinition; f <= FeatureOrganiseImports; f++ {
		if name := f.String(); name == "unknown" || strings.Contains(name, "/") {
			t.Fatalf("feature %d is named %q", f, name)
		}
	}
}

func TestAnEmptyWorkspaceEditChangesNothing(t *testing.T) {
	// What a rename to the name a symbol already has answers with.
	if !(WorkspaceEdit{}).Empty() {
		t.Fatal("the zero edit is not empty")
	}
	if !(WorkspaceEdit{Files: []FileEdits{{Path: "/w/a.go"}}}).Empty() {
		t.Fatal("a file with no edits was read as a change")
	}
	full := WorkspaceEdit{Files: []FileEdits{{Path: "/w/a.go", Edits: []TextEdit{{}}}}}
	if full.Empty() {
		t.Fatal("a real change was read as empty")
	}
}

func BenchmarkApply(b *testing.B) {
	text := strings.Repeat("func f() { return nil }\n", 200)
	edits := []TextEdit{edit(span(0, 5, 0, 6), "g"), edit(span(100, 5, 100, 6), "h")}
	for b.Loop() {
		Apply(text, edits)
	}
}

func BenchmarkPositionOf(b *testing.B) {
	text := strings.Repeat("one two three\n", 200)
	for b.Loop() {
		PositionOf(text, 1500)
	}
}

func BenchmarkSupports(b *testing.B) {
	caps := Capabilities{Hover: true, Definition: true}
	for b.Loop() {
		Supports(caps, FeatureHover)
	}
}

func BenchmarkColumn(b *testing.B) {
	line := "func Something(a, b int) (string, error) {"
	for b.Loop() {
		Column(line, 20)
	}
}
