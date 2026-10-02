package lsp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// resolvingCaps is a server that offers completion AND the resolve behind it,
// which is what gopls answers with.
const resolvingCaps = `{
  "textDocumentSync":{"openClose":true,"change":2},
  "completionProvider":{"triggerCharacters":["."],"resolveProvider":true}
}`

// completing is a client whose server answers one completion method.
func completing(t *testing.T, caps, method, result string) (language.Client, *fakeServer) {
	t.Helper()
	h := start(t, Deps{Launcher: &fakeLauncher{caps: caps}})
	h.server.results[method] = result
	return h.client, h.server
}

func TestACompletionListIsReadInAllThreeOfItsShapes(t *testing.T) {
	// `CompletionList | CompletionItem[] | null`. A client that read only the
	// object form shows nothing against a server that answers with the array,
	// and the array is the older form several servers still send.
	for name, result := range map[string]string{
		"the object form": `{"isIncomplete":false,"items":[{"label":"Open","kind":3}]}`,
		"a bare array":    `[{"label":"Open","kind":3}]`,
	} {
		client, _ := completing(t, resolvingCaps, methodCompletion, result)
		a := answered(client, query(language.FeatureCompletion))
		if a.Err != nil || len(a.Completions.Items) != 1 {
			t.Fatalf("%s: Answer = %+v", name, a)
		}
		if got := a.Completions.Items[0]; got.Label != "Open" || got.Kind != language.KindFunction {
			t.Fatalf("%s: item = %+v", name, got)
		}
	}
}

func TestNothingToSuggestIsAnEmptyListRatherThanAFailure(t *testing.T) {
	client, _ := completing(t, resolvingCaps, methodCompletion, `null`)
	a := answered(client, query(language.FeatureCompletion))
	if a.Err != nil || len(a.Completions.Items) != 0 {
		t.Fatalf("Answer = %+v, want no items and no error", a)
	}
}

func TestAnIncompleteListSaysSo(t *testing.T) {
	// It decides whether the next keystroke FILTERS what is here or asks
	// again. gopls sets it constantly, and a client that filtered an
	// incomplete list locally shows a shrinking list of the wrong candidates.
	client, _ := completing(t, resolvingCaps, methodCompletion,
		`{"isIncomplete":true,"items":[{"label":"Open"}]}`)
	a := answered(client, query(language.FeatureCompletion))
	if !a.Completions.Incomplete {
		t.Fatal("an incomplete list came back as a complete one")
	}
}

func TestAnItemCarriesWhatTheRowDrawsAndWhatItTypesIn(t *testing.T) {
	client, _ := completing(t, resolvingCaps, methodCompletion, `{"items":[{
	  "label":"Println",
	  "kind":3,
	  "labelDetails":{"detail":"(a ...any) (n int, err error)"},
	  "sortText":"00000",
	  "filterText":"Println",
	  "insertText":"Println",
	  "additionalTextEdits":[{"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":0}},"newText":"import \"fmt\"\n"}],
	  "preselect":true
	}]}`)
	a := answered(client, query(language.FeatureCompletion))
	if len(a.Completions.Items) != 1 {
		t.Fatalf("Answer = %+v", a)
	}
	item := a.Completions.Items[0]
	if item.Detail != "(a ...any) (n int, err error)" {
		t.Fatalf("detail = %q; labelDetails is where a modern server puts the signature", item.Detail)
	}
	if item.SortKey() != "00000" || item.FilterKey() != "Println" {
		t.Fatalf("sort = %q filter = %q; the server's own ranking was thrown away", item.SortKey(), item.FilterKey())
	}
	if len(item.Additional) != 1 || !strings.Contains(item.Additional[0].NewText, "fmt") {
		t.Fatalf("the import that goes in with it was lost: %+v", item.Additional)
	}
	if !item.Preselect {
		t.Fatal("the server's own preselection was dropped")
	}
}

func TestAnItemWithNoSortOrFilterFallsBackToItsLabel(t *testing.T) {
	// Empty strings would sort before everything and match nothing.
	item := language.Completion{Label: "Open"}
	if item.SortKey() != "Open" || item.FilterKey() != "Open" {
		t.Fatalf("sort = %q filter = %q", item.SortKey(), item.FilterKey())
	}
}

func TestBothFormsOfAnItemsEditAreRead(t *testing.T) {
	// The two-range form is what a server sends when it means "replace this
	// much if you are overtyping". A client reading only the plain form
	// inserts at the wrong span and leaves the tail of the word behind.
	for name, edit := range map[string]string{
		"plain":          `"textEdit":{"range":{"start":{"line":1,"character":2},"end":{"line":1,"character":6}},"newText":"Println"}`,
		"insert/replace": `"textEdit":{"insert":{"start":{"line":1,"character":2},"end":{"line":1,"character":4}},"replace":{"start":{"line":1,"character":2},"end":{"line":1,"character":6}},"newText":"Println"}`,
	} {
		client, _ := completing(t, resolvingCaps, methodCompletion,
			`{"items":[{"label":"Println",`+edit+`}]}`)
		a := answered(client, query(language.FeatureCompletion))
		item := a.Completions.Items[0]
		if !item.HasReplace || item.Insert != "Println" {
			t.Fatalf("%s: item = %+v", name, item)
		}
		if item.Replace.End.Character != 6 {
			t.Fatalf("%s: replaces to character %d, want the whole word", name, item.Replace.End.Character)
		}
	}
}

func TestAnItemWithNoEditFallsBackToInsertTextThenTheLabel(t *testing.T) {
	client, _ := completing(t, resolvingCaps, methodCompletion,
		`{"items":[{"label":"Println","insertText":"Println("},{"label":"Fprintln"}]}`)
	a := answered(client, query(language.FeatureCompletion))
	if got := a.Completions.Items[0].Insert; got != "Println(" {
		t.Fatalf("insert = %q, want the server's insertText", got)
	}
	if got := a.Completions.Items[1].Insert; got != "Fprintln" {
		t.Fatalf("insert = %q, want the label", got)
	}
}

func TestASnippetIsMarkedRatherThanInsertedBlind(t *testing.T) {
	// Inserting `${1:name}` as literal text puts it in the operator's file.
	client, _ := completing(t, resolvingCaps, methodCompletion,
		`{"items":[{"label":"for","insertText":"for ${1:i} := range ${2:x} {\n\t$0\n}","insertTextFormat":2}]}`)
	a := answered(client, query(language.FeatureCompletion))
	if !a.Completions.Items[0].Snippet {
		t.Fatal("a snippet was not marked as one")
	}
}

func TestADeprecatedItemIsMarkedFromEitherSpelling(t *testing.T) {
	client, _ := completing(t, resolvingCaps, methodCompletion,
		`{"items":[{"label":"Old","deprecated":true},{"label":"Older","tags":[1]}]}`)
	a := answered(client, query(language.FeatureCompletion))
	for i, item := range a.Completions.Items {
		if !item.Deprecated {
			t.Fatalf("item %d (%s) is not marked deprecated", i, item.Label)
		}
	}
}

func TestAnAbsurdlyLongListIsBounded(t *testing.T) {
	// Asked on a keystroke and answered while the operator is still typing. A
	// server that sent a hundred thousand items would cost a frame per
	// character, and nobody reads past the first dozen.
	var b strings.Builder
	b.WriteString(`{"items":[`)
	for i := 0; i < language.MaxCompletions+50; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"label":"Item"}`)
	}
	b.WriteString(`]}`)
	client, _ := completing(t, resolvingCaps, methodCompletion, b.String())
	a := answered(client, query(language.FeatureCompletion))
	if len(a.Completions.Items) != language.MaxCompletions {
		t.Fatalf("%d items came through, want the bound", len(a.Completions.Items))
	}
}

func TestTheTriggerCharacterIsSentSoTheServerCanRankForIt(t *testing.T) {
	client, server := completing(t, resolvingCaps, methodCompletion, `{"items":[]}`)
	q := query(language.FeatureCompletion)
	q.Trigger = "."
	answered(client, q)
	for _, msg := range server.sent() {
		if msg.Method != methodCompletion {
			continue
		}
		var params completionParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			t.Fatalf("params did not decode: %v", err)
		}
		if params.Context.TriggerKind != triggerCharacter || params.Context.TriggerCharacter != "." {
			t.Fatalf("context = %+v, want the character that opened the list", params.Context)
		}
		return
	}
	t.Fatal("no completion request was sent")
}

func TestAResolveFillsTheItemInWithoutBlankingIt(t *testing.T) {
	// A resolve answer may omit everything it is not changing. A client that
	// swapped the item for what came back would blank the label under the
	// operator's own selection.
	client, _ := completing(t, resolvingCaps, methodCompletionResolve,
		`{"documentation":{"kind":"markdown","value":"Println formats using the default formats."}}`)
	q := query(language.FeatureCompletionResolve)
	q.Item = language.Completion{Label: "Println", Kind: language.KindFunction, Insert: "Println"}
	a := answered(client, q)
	if a.Err != nil {
		t.Fatalf("Err = %v", a.Err)
	}
	if a.Item.Label != "Println" || a.Item.Insert != "Println" || a.Item.Kind != language.KindFunction {
		t.Fatalf("the resolve blanked the item: %+v", a.Item)
	}
	if !strings.Contains(a.Item.Documentation, "formats using") {
		t.Fatalf("documentation = %q", a.Item.Documentation)
	}
	if !a.Item.Resolved {
		t.Fatal("the item is not marked resolved, so it will be asked about again")
	}
}

func TestAResolveHandsTheServerBackItsOwnToken(t *testing.T) {
	// `data` is the server's bookkeeping and resolve is meaningless without
	// it: gopls answers an item with no data by returning it unchanged.
	client, server := completing(t, resolvingCaps, methodCompletionResolve, `{"label":"Println"}`)
	q := query(language.FeatureCompletionResolve)
	q.Item = language.Completion{Label: "Println", Data: []byte(`{"uri":"file:///w/main.go","id":7}`)}
	answered(client, q)
	for _, msg := range server.sent() {
		if msg.Method != methodCompletionResolve {
			continue
		}
		if !strings.Contains(string(msg.Params), `"id":7`) {
			t.Fatalf("the server's own token was not handed back: %s", msg.Params)
		}
		return
	}
	t.Fatal("no resolve request was sent")
}

func TestResolveIsNotAskedOfAServerThatDoesNotOfferIt(t *testing.T) {
	// A separate capability from completion. Asking anyway comes back as a
	// protocol error number every time the operator moves down a row.
	client, server := completing(t, defaultCaps, methodCompletionResolve, `{"label":"Println"}`)
	q := query(language.FeatureCompletionResolve)
	q.Item = language.Completion{Label: "Println"}
	a := answered(client, q)
	if !errors.Is(a.Err, language.ErrUnsupported) {
		t.Fatalf("Err = %v, want ErrUnsupported", a.Err)
	}
	for _, msg := range server.sent() {
		if msg.Method == methodCompletionResolve {
			t.Fatal("resolve was sent to a server that never offered it")
		}
	}
}

func TestCompletionIsNotAskedOfAServerThatDoesNotOfferIt(t *testing.T) {
	const noCompletion = `{"textDocumentSync":{"openClose":true,"change":2},"hoverProvider":true}`
	client, server := completing(t, noCompletion, methodCompletion, `{"items":[{"label":"Open"}]}`)
	a := answered(client, query(language.FeatureCompletion))
	if !errors.Is(a.Err, language.ErrUnsupported) {
		t.Fatalf("Err = %v, want ErrUnsupported", a.Err)
	}
	for _, msg := range server.sent() {
		if msg.Method == methodCompletion {
			t.Fatal("completion was sent to a server that never offered it")
		}
	}
}
