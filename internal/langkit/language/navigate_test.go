package language

import "testing"

// onLine is a range within one line, which every name in these tests is.
func onLine(line, from, to int) Range { return span(line, from, line, to) }

func TestTheDeclarationYouAreStandingOnIsNotSomewhereToGo(t *testing.T) {
	// What gopls answers for a Ctrl+click on `total` in `func total(...)`: the
	// definition of the symbol IS the line the question was asked from.
	here := Location{Path: "/w/main.go", Range: onLine(15, 5, 10)}
	if got := Elsewhere([]Location{here}, "/w/main.go", at(15, 7)); len(got) != 0 {
		t.Fatalf("the declaration under the caret was offered as a place to go: %+v", got)
	}
}

func TestADeclarationInAnotherFileIsSomewhereToGo(t *testing.T) {
	there := Location{Path: "/w/other.go", Range: onLine(15, 5, 10)}
	got := Elsewhere([]Location{there}, "/w/main.go", at(15, 7))
	if len(got) != 1 || got[0].Path != "/w/other.go" {
		t.Fatalf("a declaration in another file was dropped: %+v", got)
	}
}

func TestTheSameLinesInDifferentFilesAreNotTheSamePlace(t *testing.T) {
	// The line and column match and the FILE does not, which is the case a
	// comparison that forgot the path gets wrong.
	same := Location{Path: "/w/other.go", Range: onLine(15, 5, 10)}
	if got := Elsewhere([]Location{same}, "/w/main.go", at(15, 7)); len(got) != 1 {
		t.Fatalf("a place in another file was taken for this one: %+v", got)
	}
}

func TestAUsageListKeepsTheUsesAndDropsTheDeclaration(t *testing.T) {
	// The whole of the second half: gopls includes the declaration in a
	// references answer, and a list that kept it would make a symbol used once
	// look like a symbol used twice.
	places := []Location{
		{Path: "/w/main.go", Range: onLine(15, 5, 10)},
		{Path: "/w/main.go", Range: onLine(24, 20, 25)},
	}
	got := Elsewhere(places, "/w/main.go", at(15, 7))
	if len(got) != 1 || got[0].Range.Start.Line != 24 {
		t.Fatalf("want the one use on line 24, got %+v", got)
	}
}

func TestAClickAtEitherEndOfTheNameIsStillOnIt(t *testing.T) {
	// A caret after the last letter resolves the same symbol, so it must not
	// read as somewhere else.
	here := Location{Path: "/w/main.go", Range: onLine(15, 5, 10)}
	for _, caret := range []Position{at(15, 5), at(15, 10)} {
		if got := Elsewhere([]Location{here}, "/w/main.go", caret); len(got) != 0 {
			t.Fatalf("a caret at %+v read as somewhere else", caret)
		}
	}
	// And one character past the end is a different place.
	if got := Elsewhere([]Location{here}, "/w/main.go", at(15, 11)); len(got) != 1 {
		t.Fatalf("a caret past the name was taken for the name: %+v", got)
	}
}

func TestARangeSpanningLinesCoversTheLinesBetween(t *testing.T) {
	block := Location{Path: "/w/main.go", Range: Range{Start: at(10, 4), End: at(14, 1)}}
	for _, caret := range []Position{at(10, 4), at(12, 0), at(14, 1)} {
		if got := Elsewhere([]Location{block}, "/w/main.go", caret); len(got) != 0 {
			t.Fatalf("a caret at %+v was outside a block that spans it", caret)
		}
	}
	if got := Elsewhere([]Location{block}, "/w/main.go", at(9, 99)); len(got) != 1 {
		t.Fatalf("a caret on the line above was taken for inside: %+v", got)
	}
}

func TestNowhereToGoIsNotAnError(t *testing.T) {
	if got := Elsewhere(nil, "/w/main.go", at(1, 1)); len(got) != 0 {
		t.Fatalf("an empty answer produced %+v", got)
	}
}
