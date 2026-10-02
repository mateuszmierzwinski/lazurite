package lsp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// asking builds a started client whose server answers one method with one
// canned result.
func asking(t *testing.T, method, result string) language.Client {
	t.Helper()
	h := start(t, Deps{})
	h.server.results[method] = result
	return h.client
}

// query is a well-formed question about the first line of a file.
func query(feature language.Feature) language.Query {
	return language.Query{Feature: feature, Path: "/w/main.go"}
}

// answered asks with a deadline, so a wedged fake cannot hang the suite.
func answered(client language.Client, q language.Query) language.Answer {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return Ask(ctx, client, q)
}

func TestAFeatureTheServerDidNotAdvertiseIsNotAsked(t *testing.T) {
	// ASKED rather than discovered by failing: a server answers an unsupported
	// method with an error code, which reaches the operator as a number.
	h := start(t, Deps{})
	a := answered(h.client, query(language.FeatureTypeDefinition))
	if !errors.Is(a.Err, language.ErrUnsupported) {
		t.Fatalf("Err = %v, want ErrUnsupported", a.Err)
	}
	for _, msg := range h.server.sent() {
		if msg.Method == methodTypeDefinition {
			t.Fatal("the request was sent to a server that never offered it")
		}
	}
}

func TestTheThreeShapesOfAWhereIsItAnswerAreAllRead(t *testing.T) {
	// `Location | Location[] | LocationLink[]`. A client that decoded only the
	// first two answers "no definition found" against a server that sends the
	// third — and gopls sends the third.
	for name, result := range map[string]string{
		"one location": `{"uri":"file:///w/other.go","range":{"start":{"line":4,"character":1},"end":{"line":4,"character":6}}}`,
		"a list":       `[{"uri":"file:///w/other.go","range":{"start":{"line":4,"character":1},"end":{"line":4,"character":6}}}]`,
		"links": `[{"targetUri":"file:///w/other.go",` +
			`"targetRange":{"start":{"line":2,"character":0},"end":{"line":9,"character":0}},` +
			`"targetSelectionRange":{"start":{"line":4,"character":1},"end":{"line":4,"character":6}}}]`,
	} {
		client := asking(t, methodDefinition, result)
		a := answered(client, query(language.FeatureDefinition))
		if a.Err != nil || len(a.Locations) != 1 {
			t.Fatalf("%s: Answer = %+v", name, a)
		}
		if got := a.Locations[0]; got.Path != "/w/other.go" || got.Range.Start.Line != 4 {
			t.Fatalf("%s: location = %+v, want the symbol's own line", name, got)
		}
	}
}

func TestNoDefinitionIsAnEmptyAnswerRatherThanAFailure(t *testing.T) {
	client := asking(t, methodDefinition, `null`)
	a := answered(client, query(language.FeatureDefinition))
	if a.Err != nil || len(a.Locations) != 0 {
		t.Fatalf("Answer = %+v, want no places and no error", a)
	}
}

func TestEveryShapeOfHoverProseIsRead(t *testing.T) {
	// Four live shapes, two of them deprecated and still sent. A client that
	// read only the current one shows an empty hover against a server that has
	// not moved.
	for name, contents := range map[string]string{
		"a plain string":  `"func F()"`,
		"markup content":  `{"kind":"markdown","value":"func F()"}`,
		"a marked string": `{"language":"go","value":"func F()"}`,
		"an array":        `[{"kind":"markdown","value":"func F()"}]`,
	} {
		client := asking(t, methodHover, `{"contents":`+contents+`}`)
		a := answered(client, query(language.FeatureHover))
		if a.Err != nil || !strings.Contains(a.Hover.Text, "func F()") {
			t.Fatalf("%s: hover = %+v", name, a.Hover)
		}
	}
}

func TestAHoverThatSaysNothingIsAnEmptyAnswer(t *testing.T) {
	client := asking(t, methodHover, `null`)
	a := answered(client, query(language.FeatureHover))
	if a.Err != nil || a.Hover.Text != "" || a.Hover.HasSpan {
		t.Fatalf("hover = %+v", a.Hover)
	}
}

func TestSignatureIndicesAreClampedIntoTheListTheServerSent(t *testing.T) {
	// A server's arithmetic error must not become an index past the end of a
	// slice in the window that drew it.
	client := asking(t, methodSignatureHelp,
		`{"signatures":[{"label":"F(a int)","parameters":[{"label":[2,7]}]}],`+
			`"activeSignature":9,"activeParameter":-3}`)
	a := answered(client, query(language.FeatureSignature))
	if a.Err != nil || len(a.Signatures.Signatures) != 1 {
		t.Fatalf("Answer = %+v", a)
	}
	if a.Signatures.Active != 0 || a.Signatures.Parameter != 0 {
		t.Fatalf("indices = %d, %d, want both clamped", a.Signatures.Active, a.Signatures.Parameter)
	}
	if got := a.Signatures.Signatures[0].Parameters[0]; got != "a int" {
		t.Fatalf("parameter = %q, want the label's own span resolved", got)
	}
}

func TestBothShapesOfAWorkspaceEditAreRead(t *testing.T) {
	// gopls answers a rename with documentChanges; other servers use changes.
	// A client that read one gets nothing from the other.
	edit := `{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":3}},"newText":"new"}`
	for name, result := range map[string]string{
		"changes":         `{"changes":{"file:///w/a.go":[` + edit + `]}}`,
		"documentChanges": `{"documentChanges":[{"textDocument":{"uri":"file:///w/a.go","version":1},"edits":[` + edit + `]}]}`,
	} {
		client := asking(t, methodRename, result)
		q := query(language.FeatureRename)
		q.NewName = "new"
		a := answered(client, q)
		if a.Err != nil || len(a.Edit.Files) != 1 || a.Edit.Files[0].Path != "/w/a.go" {
			t.Fatalf("%s: edit = %+v", name, a.Edit)
		}
	}
}

func TestAFileOperationInAWorkspaceEditIsNotPerformed(t *testing.T) {
	// This editor changes TEXT. A server that wants a file created, renamed or
	// deleted is asking for something the operator did not agree to when they
	// typed a new name.
	client := asking(t, methodRename,
		`{"documentChanges":[`+
			`{"kind":"delete","uri":"file:///w/gone.go"},`+
			`{"kind":"rename","oldUri":"file:///w/a.go","newUri":"file:///w/b.go"},`+
			`{"textDocument":{"uri":"file:///w/a.go","version":1},"edits":[]}]}`)
	q := query(language.FeatureRename)
	q.NewName = "new"
	a := answered(client, q)
	if len(a.Edit.Files) != 1 || a.Edit.Files[0].Path != "/w/a.go" {
		t.Fatalf("edit = %+v, want only the text change", a.Edit)
	}
}

func TestACodeActionWithNoEditOfItsOwnIsNotOffered(t *testing.T) {
	// An action whose work is a server-side COMMAND comes back as a
	// `workspace/applyEdit`, which this client refuses — so offering it would
	// be a menu entry that appears to work and does nothing.
	client := asking(t, methodCodeAction,
		`[{"title":"Run a command","command":{"command":"gopls.tidy"}},`+
			`{"title":"Disabled thing","disabled":{"reason":"no"},"edit":{"changes":{}}},`+
			`{"title":"Add import","isPreferred":true,"kind":"quickfix","edit":{"changes":{"file:///w/a.go":`+
			`[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"import x"}]}}}]`)
	a := answered(client, query(language.FeatureCodeAction))
	if a.Err != nil || len(a.Actions) != 1 {
		t.Fatalf("actions = %+v, want only the one carrying an edit", a.Actions)
	}
	if a.Actions[0].Title != "Add import" || !a.Actions[0].Preferred {
		t.Fatalf("action = %+v", a.Actions[0])
	}
}

func TestOrganiseImportsAsksForThatKindAlone(t *testing.T) {
	// It is a code action of a particular kind, not a request of its own — and
	// without the `only` filter the answer is every refactoring the server
	// knows, which the operator would have to search.
	h := start(t, Deps{})
	h.server.results[methodCodeAction] = `[]`
	answered(h.client, query(language.FeatureOrganiseImports))
	for _, msg := range h.server.sent() {
		if msg.Method != methodCodeAction {
			continue
		}
		if !strings.Contains(string(msg.Params), kindOrganiseImports) {
			t.Fatalf("the request did not narrow to the kind: %s", msg.Params)
		}
		return
	}
	t.Fatal("no code-action request was sent")
}

func TestARefactoringAsksForTheKindsItWasNamedWith(t *testing.T) {
	// What makes Extract Function an action rather than a line in a menu: the
	// window names the kind, the server answers with that change or with
	// nothing, and nobody reads a list.
	h := start(t, Deps{})
	h.server.results[methodCodeAction] = `[]`
	q := query(language.FeatureCodeAction)
	q.Only = language.ExtractFunction.Kinds()
	answered(h.client, q)
	params := codeActionParamsSent(t, h)
	for _, kind := range q.Only {
		if !strings.Contains(params, kind) {
			t.Fatalf("the request did not ask for %q: %s", kind, params)
		}
	}
}

func TestOrganiseImportsIgnoresTheKindsAQueryCarries(t *testing.T) {
	// The handler's own kind is the whole of what it is. A query that arrived
	// carrying somebody else's kinds must not turn organise-imports into a
	// request for a refactoring.
	h := start(t, Deps{})
	h.server.results[methodCodeAction] = `[]`
	q := query(language.FeatureOrganiseImports)
	q.Only = language.Inline.Kinds()
	answered(h.client, q)
	params := codeActionParamsSent(t, h)
	if !strings.Contains(params, kindOrganiseImports) || strings.Contains(params, "refactor.inline") {
		t.Fatalf("organise imports asked for %s", params)
	}
}

// codeActionParamsSent is the parameters of the code-action request the client
// sent, for a test that is about the QUESTION rather than the answer.
func codeActionParamsSent(t *testing.T, h *harness) string {
	t.Helper()
	for _, msg := range h.server.sent() {
		if msg.Method == methodCodeAction {
			return string(msg.Params)
		}
	}
	t.Fatal("no code-action request was sent")
	return ""
}

func TestAPrepareRenameThatFitsNoKnownShapeRefusesTheRename(t *testing.T) {
	// The safe direction: refusing a rename that would have worked costs a
	// keystroke, where allowing one the server cannot do produces an edit set
	// nobody can predict.
	client := asking(t, methodPrepareRename, `null`)
	a := answered(client, query(language.FeaturePrepareRename))
	if a.Err != nil || a.Rename.Allowed {
		t.Fatalf("prepare = %+v, want a refusal", a.Rename)
	}
}

func TestAPrepareRenameCarriesTheNameTheSymbolHasNow(t *testing.T) {
	client := asking(t, methodPrepareRename,
		`{"range":{"start":{"line":1,"character":4},"end":{"line":1,"character":9}},"placeholder":"Thing"}`)
	a := answered(client, query(language.FeaturePrepareRename))
	if !a.Rename.Allowed || a.Rename.Placeholder != "Thing" {
		t.Fatalf("prepare = %+v", a.Rename)
	}
}

func TestReferencesAskForTheDeclarationToo(t *testing.T) {
	// "Find references" that omitted the declaration answers a different
	// question from the one every editor's menu entry implies.
	h := start(t, Deps{})
	h.server.results[methodReferences] = `[]`
	answered(h.client, query(language.FeatureReferences))
	for _, msg := range h.server.sent() {
		if msg.Method == methodReferences {
			if !strings.Contains(string(msg.Params), `"includeDeclaration":true`) {
				t.Fatalf("the request did not ask for the declaration: %s", msg.Params)
			}
			return
		}
	}
	t.Fatal("no references request was sent")
}

func TestTheAnswerAlwaysNamesTheFeatureThatWasAsked(t *testing.T) {
	// The window reports a failure by naming what it asked for, so an answer
	// that lost that says "unknown: …" instead.
	h := start(t, Deps{})
	a := answered(h.client, query(language.FeatureImplementation))
	if a.Feature != language.FeatureImplementation {
		t.Fatalf("Feature = %v", a.Feature)
	}
}

func BenchmarkLocationsOf(b *testing.B) {
	raw := []byte(`[{"uri":"file:///w/a.go","range":{"start":{"line":1,"character":2},"end":{"line":1,"character":6}}}]`)
	for b.Loop() {
		locationsOf(raw)
	}
}
