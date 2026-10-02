package language

import "testing"

func TestEachTreeAsksItsOwnQuestions(t *testing.T) {
	// Four questions, and the window must not be able to ask a call hierarchy's
	// question about a type: a server would answer about something else, or
	// with an error code the operator reads as a broken editor.
	for _, row := range []struct {
		tree Tree
		way  Direction
		want Feature
	}{
		{Calls, Up, FeatureIncomingCalls},
		{Calls, Down, FeatureOutgoingCalls},
		{Types, Up, FeatureSupertypes},
		{Types, Down, FeatureSubtypes},
	} {
		if got := row.tree.Step(row.way); got != row.want {
			t.Errorf("tree %d %s asks %v, want %v", int(row.tree), row.way, got, row.want)
		}
	}
	if Calls.Prepare() != FeaturePrepareCallHierarchy || Types.Prepare() != FeaturePrepareTypeHierarchy {
		t.Fatal("a tree prepares with the other tree's request")
	}
}

func TestAHierarchyNeedsItsOwnCapability(t *testing.T) {
	// A server that speaks one hierarchy and not the other is ordinary, and
	// asking anyway reaches the operator as a protocol error number.
	calls := Capabilities{CallHierarchy: true}
	types := Capabilities{TypeHierarchy: true}
	for _, feature := range []Feature{
		FeaturePrepareCallHierarchy, FeatureIncomingCalls, FeatureOutgoingCalls,
	} {
		if !Supports(calls, feature) || Supports(types, feature) {
			t.Errorf("%v is covered by the wrong capability", feature)
		}
	}
	for _, feature := range []Feature{
		FeaturePrepareTypeHierarchy, FeatureSupertypes, FeatureSubtypes,
	} {
		if !Supports(types, feature) || Supports(calls, feature) {
			t.Errorf("%v is covered by the wrong capability", feature)
		}
	}
}

func TestAnEmptyItemIsRecognisedAsNothing(t *testing.T) {
	// What a prepare on a keyword or a comment answers with. A pane that showed
	// it would draw an empty row nobody can open.
	if !(HierarchyItem{}).Zero() {
		t.Fatal("the zero item names something")
	}
	if (HierarchyItem{Name: "total", Path: "/w/main.go"}).Zero() {
		t.Fatal("a real item is read as nothing")
	}
}

func TestEveryFeatureThisEditorAsksHasAName(t *testing.T) {
	// The name is what a failure reaches the operator as. One that fell through
	// to "unknown" would be a status bar saying the unknown feature failed.
	for feature := FeatureDefinition; feature <= FeatureSubtypes; feature++ {
		if feature.String() == "unknown" {
			t.Errorf("feature %d has no name", int(feature))
		}
	}
}
