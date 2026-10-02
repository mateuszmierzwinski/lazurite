package debug

import "testing"

// watching is a list with these expressions in it.
func watching(t *testing.T, expressions ...string) *Watches {
	t.Helper()
	w := &Watches{}
	for _, expression := range expressions {
		if !w.Add(expression) {
			t.Fatalf("%q was refused", expression)
		}
	}
	return w
}

func TestAnExpressionIsKeptExactlyAsItWasTyped(t *testing.T) {
	// A debugger's language is not this editor's to normalise: `len(xs)` and
	// `len( xs )` may both be valid and only one of them is what was written.
	w := watching(t, "len(xs)")
	if got := w.List()[0].Expression; got != "len(xs)" {
		t.Fatalf("kept %q", got)
	}
}

func TestTheSameExpressionIsNotWatchedTwice(t *testing.T) {
	// Two identical rows answer the same question twice on every stop and give
	// the operator two lines to keep in step by hand.
	w := watching(t, "err")
	if w.Add("err") || w.Add("  err  ") {
		t.Fatalf("a duplicate was added: %+v", w.List())
	}
	if w.Len() != 1 {
		t.Fatalf("len = %d", w.Len())
	}
}

func TestAnEmptyExpressionIsRefused(t *testing.T) {
	w := &Watches{}
	if w.Add("   ") || w.Len() != 0 {
		t.Fatalf("an empty expression was watched: %+v", w.List())
	}
}

func TestTheListIsBounded(t *testing.T) {
	// Every watch is a request to the adapter on every stop, and a stop that
	// answered hundreds before the stack appeared would make stepping feel
	// broken.
	w := &Watches{}
	for i := 0; i < MaxWatches+10; i++ {
		w.Add(string(rune('a'+i%26)) + string(rune('0'+i/26)))
	}
	if w.Len() != MaxWatches {
		t.Fatalf("len = %d, want the bound", w.Len())
	}
}

func TestAnAnswerLandsOnItsOwnRow(t *testing.T) {
	// Answers arrive on their own goroutine and the operator may have added or
	// removed a row in between: one written by index would land on somebody
	// else's line.
	w := watching(t, "a", "b", "c")
	w.Remove("a")
	w.Answer("c", Variable{Name: "c", Value: "3"}, "")
	list := w.List()
	if list[1].Expression != "c" || list[1].Value.Value != "3" {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Value.Value != "" {
		t.Fatalf("an answer landed on %q", list[0].Expression)
	}
}

func TestAnAnswerForSomethingNoLongerWatchedIsDropped(t *testing.T) {
	w := watching(t, "a")
	w.Remove("a")
	w.Answer("a", Variable{Value: "1"}, "")
	if w.Len() != 0 {
		t.Fatalf("list = %+v", w.List())
	}
}

func TestAFailedEvaluationIsAnAnswerLikeAnyOther(t *testing.T) {
	// An expression is written before the frame that would answer it exists,
	// and `undefined: x` is the answer rather than an exception.
	w := watching(t, "x")
	w.Answer("x", Variable{}, "undefined: x")
	got := w.List()[0]
	if got.Err != "undefined: x" || !got.Fresh {
		t.Fatalf("watch = %+v", got)
	}
}

func TestResumingLeavesTheValuesUpAndMarksThemStale(t *testing.T) {
	// A list that emptied itself on Continue would be useless for the one thing
	// it is for: watching a value change as the program runs.
	w := watching(t, "i")
	w.Answer("i", Variable{Value: "7"}, "")
	w.Stale()
	got := w.List()[0]
	if got.Value.Value != "7" {
		t.Fatalf("the value was cleared: %+v", got)
	}
	if got.Fresh {
		t.Fatal("a value from the last stop is claiming to be from this one")
	}
}

func TestRemovingByRowTakesTheRightOne(t *testing.T) {
	w := watching(t, "a", "b")
	if !w.RemoveAt(0) || w.Len() != 1 || w.List()[0].Expression != "b" {
		t.Fatalf("list = %+v", w.List())
	}
	if w.RemoveAt(9) {
		t.Fatal("a row that does not exist was removed")
	}
}
