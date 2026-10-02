package lsp

import (
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// resolvingActionCaps is a server that keeps its refactorings back until they
// are chosen, which is what gopls answers with.
const resolvingActionCaps = `{
  "textDocumentSync":{"openClose":true,"change":2},
  "codeActionProvider":{"codeActionKinds":["refactor.extract.function"],"resolveProvider":true}
}`

// keptBack is the answer gopls gives to a refactoring request: a name, a kind
// and a token, and no edit at all.
const keptBack = `[{"title":"Extract function","kind":"refactor.extract.function",` +
	`"data":{"uri":"file:///w/main.go","rng":{"start":{"line":7,"character":0}}}}]`

func TestAnActionTheServerKeptBackIsStillOffered(t *testing.T) {
	// The whole of what was wrong: gopls answers every refactoring with a token
	// and no edit, this client dropped anything with no edit, and the window
	// said the server had nothing to offer — against a server that was offering
	// exactly what had been asked for.
	client := asking(t, methodCodeAction, keptBack)
	a := answered(client, query(language.FeatureCodeAction))
	if a.Err != nil || len(a.Actions) != 1 {
		t.Fatalf("actions = %+v (%v)", a.Actions, a.Err)
	}
	if !a.Actions[0].NeedsResolve() {
		t.Fatalf("action = %+v, want one waiting to be asked about", a.Actions[0])
	}
}

func TestAnActionWithNeitherAnEditNorATokenIsStillDropped(t *testing.T) {
	// The command form has not become acceptable: its work happens server-side
	// and comes back as a `workspace/applyEdit` this client refuses, which is a
	// menu entry that appears to work and does nothing.
	client := asking(t, methodCodeAction,
		`[{"title":"Run a command","kind":"source.test","command":{"command":"gopls.test"}}]`)
	a := answered(client, query(language.FeatureCodeAction))
	if len(a.Actions) != 0 {
		t.Fatalf("actions = %+v, want none this editor can carry out", a.Actions)
	}
}

func TestResolvingAnActionAsksWithTheServersOwnTokenAndFillsTheEdit(t *testing.T) {
	h := start(t, Deps{Launcher: &fakeLauncher{caps: resolvingActionCaps}})
	h.server.results[methodCodeActionResolve] = `{"title":"Extract function",` +
		`"kind":"refactor.extract.function","edit":{"changes":{"file:///w/main.go":` +
		`[{"range":{"start":{"line":7,"character":0},"end":{"line":7,"character":21}},` +
		`"newText":"\ttotal = newFunction(total, i)"}]}}}`

	q := query(language.FeatureCodeActionResolve)
	q.Action = language.CodeAction{
		Title: "Extract function", Kind: "refactor.extract.function",
		Data: []byte(`{"uri":"file:///w/main.go"}`),
	}
	a := answered(h.client, q)
	if a.Err != nil {
		t.Fatalf("resolve = %v", a.Err)
	}
	if len(a.Action.Edit.Files) != 1 || len(a.Action.Edit.Files[0].Edits) != 1 {
		t.Fatalf("the resolved action changes %+v", a.Action.Edit)
	}
	if a.Action.Title != "Extract function" {
		t.Fatalf("the resolved action is called %q", a.Action.Title)
	}
	sent := requestParams(t, h, methodCodeActionResolve)
	if !strings.Contains(sent, `"file:///w/main.go"`) {
		t.Fatalf("the server was not handed back its own token: %s", sent)
	}
}

func TestAServerThatDoesNotResolveIsNotAsked(t *testing.T) {
	// A capability of its own. `codeActionProvider: true` is a server that
	// answers with whole actions, and `codeAction/resolve` would come back as
	// a protocol error number.
	h := start(t, Deps{})
	q := query(language.FeatureCodeActionResolve)
	q.Action = language.CodeAction{Title: "Extract function", Data: []byte(`{}`)}
	a := answered(h.client, q)
	if a.Err != language.ErrUnsupported {
		t.Fatalf("Err = %v, want ErrUnsupported", a.Err)
	}
	for _, msg := range h.server.sent() {
		if msg.Method == methodCodeActionResolve {
			t.Fatal("a resolve was sent to a server that never offered it")
		}
	}
}

func TestTheResolveCapabilityIsReadFromTheProviderObject(t *testing.T) {
	for name, row := range map[string]struct {
		caps string
		want bool
	}{
		"an options object saying so": {resolvingActionCaps, true},
		"a bare true":                 {defaultCaps, false},
	} {
		h := start(t, Deps{Launcher: &fakeLauncher{caps: row.caps}})
		if got := h.client.Capabilities().CodeActionResolve; got != row.want {
			t.Errorf("%s: CodeActionResolve = %v, want %v", name, got, row.want)
		}
	}
}

func TestThisClientTellsTheServerItWillAskForAnEditLater(t *testing.T) {
	// The declaration is what decides which SHAPE the refactorings arrive in: a
	// client that does not make it is offered them as commands it cannot run,
	// and shows an empty menu against a server that had plenty to offer. It is
	// asserted on the wire because that is where it went wrong.
	h := start(t, Deps{})
	params := requestParams(t, h, "initialize")
	if !strings.Contains(params, `"resolveSupport":{"properties":["edit"]}`) {
		t.Fatalf("initialize did not declare code-action resolve: %s", params)
	}
	if !strings.Contains(params, `"dataSupport":true`) {
		t.Fatalf("initialize did not declare data support: %s", params)
	}
}

// requestParams is the parameters of the first request the client sent for one
// method.
func requestParams(t *testing.T, h *harness, method string) string {
	t.Helper()
	for _, msg := range h.server.sent() {
		if msg.Method == method {
			return string(msg.Params)
		}
	}
	t.Fatalf("no %s request was sent", method)
	return ""
}
