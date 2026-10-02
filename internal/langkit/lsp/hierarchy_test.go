package lsp

import (
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// hierarchyCaps is a server that speaks both trees, which is what gopls answers
// with.
const hierarchyCaps = `{
  "textDocumentSync":{"openClose":true,"change":2},
  "callHierarchyProvider":true,
  "typeHierarchyProvider":true
}`

// preparedItem is what a prepare answers with: a name, a place, and the
// server's own token.
const preparedItem = `[{"name":"total","kind":12,"detail":"main",` +
	`"uri":"file:///w/main.go",` +
	`"range":{"start":{"line":4,"character":0},"end":{"line":10,"character":1}},` +
	`"selectionRange":{"start":{"line":4,"character":5},"end":{"line":4,"character":10}},` +
	`"data":{"id":"total"}}]`

// hierarchyClient is a started client whose server answers one method.
func hierarchyClient(t *testing.T, method, result string) (language.Client, *fakeServer) {
	t.Helper()
	h := start(t, Deps{Launcher: &fakeLauncher{caps: hierarchyCaps}})
	h.server.results[method] = result
	return h.client, h.server
}

func TestAPrepareAnswersWithTheItemAndItsToken(t *testing.T) {
	client, _ := hierarchyClient(t, methodPrepareCallHierarchy, preparedItem)
	a := answered(client, query(language.FeaturePrepareCallHierarchy))
	if a.Err != nil || len(a.Roots) != 1 {
		t.Fatalf("roots = %+v (%v)", a.Roots, a.Err)
	}
	root := a.Roots[0]
	if root.Name != "total" || root.Path != "/w/main.go" || root.Kind != 12 {
		t.Fatalf("root = %+v", root)
	}
	if root.Selection.Start.Line != 4 || root.Selection.Start.Character != 5 {
		t.Fatalf("the row would open at %+v, want the name", root.Selection)
	}
	if len(root.Data) == 0 {
		t.Fatal("the server's own token was thrown away")
	}
}

func TestIncomingCallsCarryWhereTheCallIsWritten(t *testing.T) {
	// The ranges are what make a call hierarchy more than a list of functions:
	// a caller that calls this three times is one row and three places.
	client, _ := hierarchyClient(t, methodIncomingCalls,
		`[{"from":{"name":"main","kind":12,"uri":"file:///w/main.go",`+
			`"range":{"start":{"line":12,"character":0},"end":{"line":15,"character":1}},`+
			`"selectionRange":{"start":{"line":12,"character":5},"end":{"line":12,"character":9}}},`+
			`"fromRanges":[{"start":{"line":13,"character":12},"end":{"line":13,"character":17}},`+
			`{"start":{"line":14,"character":4},"end":{"line":14,"character":9}}]}]`)
	a := answered(client, query(language.FeatureIncomingCalls))
	if a.Err != nil || len(a.Edges) != 1 {
		t.Fatalf("edges = %+v (%v)", a.Edges, a.Err)
	}
	if a.Edges[0].Item.Name != "main" || len(a.Edges[0].Ranges) != 2 {
		t.Fatalf("edge = %+v", a.Edges[0])
	}
}

func TestOutgoingCallsReadTheOtherEndOfTheSameShape(t *testing.T) {
	// `from` and `to` are the same field in opposite directions, and a client
	// that read only one answers "this calls nothing" for every function.
	client, _ := hierarchyClient(t, methodOutgoingCalls,
		`[{"to":{"name":"Println","kind":12,"uri":"file:///w/main.go",`+
			`"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":10}},`+
			`"selectionRange":{"start":{"line":1,"character":0},"end":{"line":1,"character":7}}},`+
			`"fromRanges":[]}]`)
	a := answered(client, query(language.FeatureOutgoingCalls))
	if a.Err != nil || len(a.Edges) != 1 || a.Edges[0].Item.Name != "Println" {
		t.Fatalf("edges = %+v (%v)", a.Edges, a.Err)
	}
}

func TestATypeHierarchyAnswersWithBareItems(t *testing.T) {
	// A supertype is not called from anywhere, so the answer is items rather
	// than edges — and the window draws both the same way.
	client, _ := hierarchyClient(t, methodSupertypes,
		`[{"name":"Reader","kind":11,"detail":"io","uri":"file:///w/io.go",`+
			`"range":{"start":{"line":2,"character":0},"end":{"line":4,"character":1}},`+
			`"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":11}}}]`)
	a := answered(client, query(language.FeatureSupertypes))
	if a.Err != nil || len(a.Edges) != 1 || a.Edges[0].Item.Name != "Reader" {
		t.Fatalf("edges = %+v (%v)", a.Edges, a.Err)
	}
	if len(a.Edges[0].Ranges) != 0 {
		t.Fatalf("a supertype came back with call sites: %+v", a.Edges[0].Ranges)
	}
}

func TestAStepHandsTheServerBackItsOwnItem(t *testing.T) {
	// A node three levels down names a file this window has never opened. The
	// question is about the ITEM, and a server given a re-invented one would
	// answer about something else.
	client, server := hierarchyClient(t, methodIncomingCalls, `[]`)
	q := query(language.FeatureIncomingCalls)
	q.Node = language.HierarchyItem{
		Name: "total", Path: "/w/deep/other.go", Data: []byte(`{"id":"total"}`),
		Selection: language.Range{Start: language.Position{Line: 4, Character: 5}},
	}
	answered(client, q)
	for _, msg := range server.sent() {
		if msg.Method != methodIncomingCalls {
			continue
		}
		params := string(msg.Params)
		if !strings.Contains(params, "/w/deep/other.go") || !strings.Contains(params, `"id":"total"`) {
			t.Fatalf("the server was asked with %s", params)
		}
		return
	}
	t.Fatal("no request was sent")
}

func TestANodeWhoseFileCannotBeReadIsDropped(t *testing.T) {
	// A row that opens nothing is worse than a row that is not there.
	client, _ := hierarchyClient(t, methodPrepareCallHierarchy,
		`[{"name":"total","uri":"http://example.com/main.go",`+
			`"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}},`+
			`{"name":"","uri":"file:///w/main.go",`+
			`"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}]`)
	a := answered(client, query(language.FeaturePrepareCallHierarchy))
	if len(a.Roots) != 0 {
		t.Fatalf("roots = %+v, want none this editor can open", a.Roots)
	}
}

func TestAServerWithNoHierarchyIsNotAsked(t *testing.T) {
	h := start(t, Deps{})
	for _, feature := range []language.Feature{
		language.FeaturePrepareCallHierarchy, language.FeatureSubtypes,
	} {
		if a := answered(h.client, query(feature)); a.Err != language.ErrUnsupported {
			t.Fatalf("%v = %v, want ErrUnsupported", feature, a.Err)
		}
	}
	for _, msg := range h.server.sent() {
		if strings.Contains(msg.Method, "Hierarchy") || strings.Contains(msg.Method, "types") {
			t.Fatalf("%s was sent to a server that never offered it", msg.Method)
		}
	}
}
