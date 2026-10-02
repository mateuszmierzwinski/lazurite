package language

import (
	"strings"
	"testing"
)

func TestEveryNamedRefactoringAsksForSomething(t *testing.T) {
	// A refactoring with no kinds asks for EVERY code action the server knows,
	// which is the Quick Fix menu under a name that promised one change.
	for _, which := range AllRefactorings {
		if !which.Known() {
			t.Errorf("%d is listed and has no row", int(which))
			continue
		}
		if which.String() == "" {
			t.Errorf("%d has no name for a message", int(which))
		}
		if len(which.Kinds()) == 0 {
			t.Errorf("%s asks for nothing, which asks for everything", which)
		}
	}
}

func TestARefactoringAsksForLeafKindsRatherThanAFamily(t *testing.T) {
	// Kinds are hierarchical: "refactor.extract" subsumes
	// "refactor.extract.function". Asking for the family answers Extract
	// Variable with an extracted function and a menu to choose from — which is
	// the thing these actions exist to replace.
	for _, which := range AllRefactorings {
		for _, kind := range which.Kinds() {
			if strings.Count(kind, ".") < 2 {
				t.Errorf("%s asks for %q, which is a family and not one change", which, kind)
			}
		}
	}
}

func TestNoTwoRefactoringsAskForTheSameKind(t *testing.T) {
	// Two that overlap are two keys that do the same thing on one press and
	// different things on another, depending on what the caret is over.
	claimed := map[string]Refactoring{}
	for _, which := range AllRefactorings {
		for _, kind := range which.Kinds() {
			if taken, clash := claimed[kind]; clash {
				t.Errorf("%s and %s both ask for %q", which, taken, kind)
			}
			claimed[kind] = which
		}
	}
}

func TestExtractionIsAboutASelectionAndInliningIsNot(t *testing.T) {
	// A server refuses an empty range by offering nothing, so the window checks
	// first and says what is missing. Inlining is about the call the caret is
	// in and needs no selection at all.
	for which, want := range map[Refactoring]bool{
		ExtractFunction: true, ExtractVariable: true, ExtractConstant: true,
		Inline: false, ChangeSignature: false,
	} {
		if got := which.NeedsSelection(); got != want {
			t.Errorf("%s needs a selection: %v, want %v", which, got, want)
		}
	}
}

func TestAValueThatNamesNoRefactoringAsksForNothing(t *testing.T) {
	// The safe direction. A zero-valued or out-of-range Refactoring reaching
	// the query builder must not become a request with an empty `only`, which
	// is the protocol's way of asking for every action there is.
	stray := Refactoring(len(AllRefactorings) + 7)
	if stray.Known() {
		t.Fatal("a value with no row was accepted")
	}
	if len(stray.Kinds()) != 0 || stray.String() != "" {
		t.Fatalf("stray refactoring answered %q / %v", stray.String(), stray.Kinds())
	}
}
