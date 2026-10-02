package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

func TestPathToURIEscapesWhatWouldSplitTheURI(t *testing.T) {
	for path, want := range map[string]string{
		"/home/user/main.go":     "file:///home/user/main.go",
		"/home/user/a b.go":      "file:///home/user/a%20b.go",
		"/home/user/a#frag.go":   "file:///home/user/a%23frag.go",
		"relative/main.go":       "file:///relative/main.go",
		"/home/user/../user/x.c": "file:///home/user/x.c",
	} {
		if got := pathToURI(path); got != want {
			t.Fatalf("pathToURI(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestURIToPathRoundTrips(t *testing.T) {
	for _, path := range []string{"/home/user/main.go", "/home/user/a b.go", "/a/#/b.go"} {
		got, err := uriToPath(pathToURI(path))
		if err != nil || got != path {
			t.Fatalf("round trip of %q = %q, %v", path, got, err)
		}
	}
}

func TestURIToPathRefusesAnythingThatIsNotALocalFile(t *testing.T) {
	// A URI in a server's answer is input, not a fact. Following one that
	// names a scheme or a host would take the editor somewhere the operator
	// never asked to go.
	for _, uri := range []string{
		"http://example.com/main.go",
		"https://example.com/main.go",
		"file://elsewhere/etc/shadow",
		"jar:file:///a.jar!/x.class",
		"",
		"file://",
		"::not a uri::",
	} {
		if _, err := uriToPath(uri); !errors.Is(err, ErrBadURI) {
			t.Fatalf("uriToPath(%q) = %v, want ErrBadURI", uri, err)
		}
	}
	// localhost is the one host the protocol allows, and it means the same
	// machine.
	if _, err := uriToPath("file://localhost/etc/hosts"); err != nil {
		t.Fatalf("uriToPath refused file://localhost: %v", err)
	}
}

func TestProvidedReadsBothFormsOfACapability(t *testing.T) {
	for _, raw := range []string{"true", `{"workDoneProgress":true}`, `{}`, `[]`} {
		if !provided(json.RawMessage(raw)) {
			t.Fatalf("provided(%s) = false; an options object IS an offer", raw)
		}
	}
}

func TestProvidedRefusesAnAbsentCapability(t *testing.T) {
	for _, raw := range []string{"", "false", "null", "  null  "} {
		if provided(json.RawMessage(raw)) {
			t.Fatalf("provided(%q) = true", raw)
		}
	}
}

func TestSyncOfReadsTheNumberForm(t *testing.T) {
	got := syncOf(json.RawMessage("2"))
	if got.Kind != int(language.SyncIncremental) || !got.OpenClose || got.Save {
		t.Fatalf("syncOf(2) = %+v", got)
	}
	if none := syncOf(json.RawMessage("0")); none.OpenClose {
		t.Fatalf("syncOf(0) = %+v; no sync means no open/close either", none)
	}
}

func TestSyncOfReadsTheObjectFormAndSurvivesRubbish(t *testing.T) {
	got := syncOf(json.RawMessage(`{"openClose":true,"change":1,"save":{"includeText":true}}`))
	if got.Kind != int(language.SyncFull) || !got.OpenClose || !got.Save || !got.IncludeText {
		t.Fatalf("syncOf(object) = %+v", got)
	}
	// A capability this client cannot read must not silently become "no sync",
	// which would leave the server never told about a single edit.
	fallback := syncOf(json.RawMessage(`"nonsense"`))
	if fallback.Kind != int(language.SyncFull) || !fallback.OpenClose {
		t.Fatalf("syncOf(rubbish) = %+v, want the safe full-sync fallback", fallback)
	}
}

func TestIncludesTextReadsTheSaveOptions(t *testing.T) {
	if !includesText(json.RawMessage(`{"includeText":true}`)) {
		t.Fatal("includesText missed an explicit request for the text")
	}
	for _, raw := range []string{`{"includeText":false}`, `true`, `null`, ``} {
		if includesText(json.RawMessage(raw)) {
			t.Fatalf("includesText(%q) = true", raw)
		}
	}
}

func TestPrepareRenameOfReadsTheRenameOptions(t *testing.T) {
	if !prepareRenameOf(json.RawMessage(`{"prepareProvider":true}`)) {
		t.Fatal("prepareRenameOf missed the prepare step")
	}
	for _, raw := range []string{`true`, `{}`, `null`, ``} {
		if prepareRenameOf(json.RawMessage(raw)) {
			t.Fatalf("prepareRenameOf(%q) = true", raw)
		}
	}
}

func TestCapabilitiesOfTranslatesAServersAnswer(t *testing.T) {
	caps := capabilitiesOf(json.RawMessage(defaultCaps))
	if caps.Sync != language.SyncIncremental || !caps.OpenClose {
		t.Fatalf("sync = %v, openClose = %v", caps.Sync, caps.OpenClose)
	}
	if !caps.Hover || !caps.Definition || !caps.References || !caps.Rename || !caps.PrepareRename {
		t.Fatalf("caps = %+v; an options object is still an offer", caps)
	}
	if len(caps.CompletionTrigger) != 2 || caps.CompletionTrigger[0] != "." {
		t.Fatalf("completion triggers = %v", caps.CompletionTrigger)
	}
	if len(caps.SignatureTrigger) != 2 || !caps.SemanticTokens || !caps.InlayHint {
		t.Fatalf("caps = %+v", caps)
	}
	if len(caps.Raw) == 0 {
		t.Fatal("Raw was dropped; it is what a later task reads an unnamed capability from")
	}
}

func TestCapabilitiesOfSupportsNothingWhenItCannotRead(t *testing.T) {
	// A server whose answer this client cannot read is a server whose features
	// must not be offered.
	for _, raw := range []string{`"not an object"`, `[1,2,3]`, ``} {
		caps := capabilitiesOf(json.RawMessage(raw))
		if caps.Hover || caps.Definition || caps.Completion || caps.Sync != language.SyncNone {
			t.Fatalf("capabilitiesOf(%q) = %+v, want the zero value", raw, caps)
		}
	}
	// An empty object is a server that offers nothing, which is legal.
	if caps := capabilitiesOf(json.RawMessage(`{}`)); caps.Hover || caps.Rename {
		t.Fatalf("capabilitiesOf({}) = %+v", caps)
	}
}

func TestQueryCapabilitiesReadsTheFlatProviders(t *testing.T) {
	caps := queryCapabilities(serverCapabilities{
		Hover:      json.RawMessage("true"),
		Definition: json.RawMessage("false"),
		Completion: &triggerProvider{TriggerCharacters: []string{"."}},
	})
	if !caps.Hover || caps.Definition || !caps.Completion {
		t.Fatalf("queryCapabilities = %+v", caps)
	}
	if empty := queryCapabilities(serverCapabilities{}); empty.Hover || empty.Completion {
		t.Fatalf("an empty capability set offered %+v", empty)
	}
}

func TestTriggersOfHandlesAnAbsentProvider(t *testing.T) {
	if got := triggersOf(&triggerProvider{TriggerCharacters: []string{"(", ","}}); len(got) != 2 {
		t.Fatalf("triggersOf = %v", got)
	}
	if got := triggersOf(nil); got != nil {
		t.Fatalf("triggersOf(nil) = %v, want nil", got)
	}
}

func TestChangesOfCarriesBothKindsOfEdit(t *testing.T) {
	span := language.Range{Start: language.Position{Line: 1}, End: language.Position{Line: 1, Character: 4}}
	got := changesOf([]language.Change{{Text: "whole"}, {Range: &span, Text: "part"}})
	if len(got) != 2 || got[0].Range != nil {
		t.Fatalf("changesOf = %+v; a whole-document change carries no range", got)
	}
	if got[1].Range == nil || got[1].Range.End.Character != 4 {
		t.Fatalf("changesOf lost the range: %+v", got[1])
	}
	if empty := changesOf(nil); len(empty) != 0 {
		t.Fatalf("changesOf(nil) = %v", empty)
	}
}

func TestWireRangeOfMapsBothWays(t *testing.T) {
	span := language.Range{Start: language.Position{Line: 3, Character: 7}}
	got := wireRangeOf(&span)
	if got == nil || got.Start.Line != 3 || got.Start.Character != 7 {
		t.Fatalf("wireRangeOf = %+v", got)
	}
	if wireRangeOf(nil) != nil {
		t.Fatal("wireRangeOf(nil) must stay nil; that is what means whole-document")
	}
}

func TestOpenParamsOfCarriesTheDocument(t *testing.T) {
	got := openParamsOf(language.Document{Path: "/w/main.go", Version: 3, Text: "package main"})
	if got.TextDocument.URI != "file:///w/main.go" || got.TextDocument.LanguageID != "go" {
		t.Fatalf("openParamsOf = %+v", got.TextDocument)
	}
	if got.TextDocument.Version != 3 || got.TextDocument.Text != "package main" {
		t.Fatalf("openParamsOf = %+v", got.TextDocument)
	}
	blank := openParamsOf(language.Document{Path: "/w/Makefile"})
	if blank.TextDocument.LanguageID != "plaintext" {
		t.Fatalf("openParamsOf = %+v", blank.TextDocument)
	}
}

func TestChangeParamsOfCarriesTheVersion(t *testing.T) {
	got := changeParamsOf("/w/main.go", 9, language.FullChange("x"))
	if got.TextDocument.Version != 9 || got.TextDocument.URI != "file:///w/main.go" {
		t.Fatalf("changeParamsOf = %+v", got.TextDocument)
	}
	if len(got.Changes) != 1 || got.Changes[0].Text != "x" {
		t.Fatalf("changeParamsOf = %+v", got.Changes)
	}
	if none := changeParamsOf("/w/main.go", 1, nil); len(none.Changes) != 0 {
		t.Fatalf("changeParamsOf(nil) = %+v", none.Changes)
	}
}

func TestSaveParamsOfSendsTheTextOnlyWhenAsked(t *testing.T) {
	if got := saveParamsOf("/w/main.go", "body", true); got.Text != "body" {
		t.Fatalf("saveParamsOf = %+v; the server asked for the text", got)
	}
	if got := saveParamsOf("/w/main.go", "body", false); got.Text != "" {
		t.Fatalf("saveParamsOf = %+v; the server did not ask for the text", got)
	}
}

func TestLanguageIDOfPrefersWhatTheCallerSaid(t *testing.T) {
	if got := languageIDOf(language.Document{Path: "/w/x.txt", LanguageID: "go"}); got != "go" {
		t.Fatalf("languageIDOf = %q; the caller's answer wins", got)
	}
	if got := languageIDOf(language.Document{Path: "/w/x.rs"}); got != "rust" {
		t.Fatalf("languageIDOf = %q", got)
	}
}

func TestIsWholeDocumentDistinguishesTheTwoSyncKinds(t *testing.T) {
	if !isWholeDocument(language.FullChange("all of it")) {
		t.Fatal("a single unranged change IS a whole document")
	}
	span := language.Range{}
	for _, changes := range [][]language.Change{
		nil,
		{{Range: &span, Text: "part"}},
		{{Text: "a"}, {Text: "b"}},
	} {
		if isWholeDocument(changes) {
			t.Fatalf("isWholeDocument(%+v) = true", changes)
		}
	}
}

func TestDecodeResultToleratesNullAndAnUninterestedCaller(t *testing.T) {
	var out struct {
		Name string `json:"name"`
	}
	if err := decodeResult(json.RawMessage(`{"name":"x"}`), &out); err != nil || out.Name != "x" {
		t.Fatalf("decodeResult = %v, %+v", err, out)
	}
	if err := decodeResult(json.RawMessage(`null`), nil); err != nil {
		t.Fatalf("decodeResult with no destination = %v, want nil", err)
	}
	if err := decodeResult(nil, &out); err != nil {
		t.Fatalf("decodeResult of an empty result = %v, want nil", err)
	}
}

func TestDecodeResultReportsAnUndecodableAnswer(t *testing.T) {
	var out struct {
		Name string `json:"name"`
	}
	if err := decodeResult(json.RawMessage(`{"name":42}`), &out); err == nil {
		t.Fatal("decodeResult accepted an answer of the wrong shape")
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	if got := backoff(1); got != baseBackoff {
		t.Fatalf("backoff(1) = %v, want %v", got, baseBackoff)
	}
	if backoff(3) <= backoff(2) {
		t.Fatalf("backoff did not grow: %v then %v", backoff(2), backoff(3))
	}
	for _, attempt := range []int{20, 1000, 1 << 20} {
		got := backoff(attempt)
		if got != maxBackoff {
			t.Fatalf("backoff(%d) = %v, want the cap %v", attempt, got, maxBackoff)
		}
	}
}

func TestBackoffIsNeverNegativeOrZero(t *testing.T) {
	// A shift that overflowed would wrap to a negative duration and restart
	// instantly, which is the opposite of what a backoff is for.
	for _, attempt := range []int{-5, 0, 1, 64, 1 << 30} {
		if got := backoff(attempt); got <= 0 {
			t.Fatalf("backoff(%d) = %v", attempt, got)
		}
	}
}

func TestDefaultsFillInWhatWasNotConfigured(t *testing.T) {
	if launcherOr(nil) == nil {
		t.Fatal("launcherOr(nil) must be the process launcher")
	}
	supplied := &fakeLauncher{}
	if launcherOr(supplied) != Launcher(supplied) {
		t.Fatal("launcherOr replaced the launcher it was given")
	}
	if lifetimeOr(nil) == nil {
		t.Fatal("lifetimeOr(nil) must be a usable context")
	}
	ctx := context.Background()
	if lifetimeOr(ctx) != ctx {
		t.Fatal("lifetimeOr replaced the context it was given")
	}
	if got := durationOr(0, time.Second); got != time.Second {
		t.Fatalf("durationOr(0) = %v", got)
	}
	if got := durationOr(-1, time.Second); got != time.Second {
		t.Fatalf("durationOr(-1) = %v; a negative bound is not a bound", got)
	}
	if got := durationOr(time.Minute, time.Second); got != time.Minute {
		t.Fatalf("durationOr = %v", got)
	}
	if got := restartsOr(0); got != defaultMaxRestarts {
		t.Fatalf("restartsOr(0) = %d", got)
	}
	if got := restartsOr(-1); got != 0 {
		t.Fatalf("restartsOr(-1) = %d; a negative budget means never restart", got)
	}
	if got := restartsOr(3); got != 3 {
		t.Fatalf("restartsOr(3) = %d", got)
	}
}

func BenchmarkPathToURI(b *testing.B) {
	for b.Loop() {
		_ = pathToURI("/home/user/project/internal/core/language/utils.go")
	}
}

func BenchmarkURIToPath(b *testing.B) {
	for b.Loop() {
		_, _ = uriToPath("file:///home/user/project/internal/core/language/utils.go")
	}
}

func BenchmarkProvided(b *testing.B) {
	raw := json.RawMessage(`{"workDoneProgress":true}`)
	for b.Loop() {
		_ = provided(raw)
	}
}

func BenchmarkSyncOf(b *testing.B) {
	raw := json.RawMessage(`{"openClose":true,"change":2,"save":{"includeText":false}}`)
	for b.Loop() {
		_ = syncOf(raw)
	}
}

func BenchmarkIncludesText(b *testing.B) {
	raw := json.RawMessage(`{"includeText":true}`)
	for b.Loop() {
		_ = includesText(raw)
	}
}

func BenchmarkPrepareRenameOf(b *testing.B) {
	raw := json.RawMessage(`{"prepareProvider":true}`)
	for b.Loop() {
		_ = prepareRenameOf(raw)
	}
}

func BenchmarkCapabilitiesOf(b *testing.B) {
	raw := json.RawMessage(defaultCaps)
	for b.Loop() {
		_ = capabilitiesOf(raw)
	}
}

func BenchmarkQueryCapabilities(b *testing.B) {
	wire := serverCapabilities{Hover: json.RawMessage("true"), Definition: json.RawMessage("true")}
	for b.Loop() {
		_ = queryCapabilities(wire)
	}
}

func BenchmarkTriggersOf(b *testing.B) {
	provider := &triggerProvider{TriggerCharacters: []string{".", "\""}}
	for b.Loop() {
		_ = triggersOf(provider)
	}
}

func BenchmarkChangesOf(b *testing.B) {
	span := language.Range{End: language.Position{Character: 4}}
	changes := []language.Change{{Range: &span, Text: "part"}}
	for b.Loop() {
		_ = changesOf(changes)
	}
}

func BenchmarkWireRangeOf(b *testing.B) {
	span := language.Range{End: language.Position{Line: 2, Character: 4}}
	for b.Loop() {
		_ = wireRangeOf(&span)
	}
}

func BenchmarkOpenParamsOf(b *testing.B) {
	doc := language.Document{Path: "/w/main.go", Version: 1, Text: "package main"}
	for b.Loop() {
		_ = openParamsOf(doc)
	}
}

func BenchmarkChangeParamsOf(b *testing.B) {
	changes := language.FullChange("package main")
	for b.Loop() {
		_ = changeParamsOf("/w/main.go", 2, changes)
	}
}

func BenchmarkSaveParamsOf(b *testing.B) {
	for b.Loop() {
		_ = saveParamsOf("/w/main.go", "package main", true)
	}
}

func BenchmarkLanguageIDOf(b *testing.B) {
	doc := language.Document{Path: "/w/main.go"}
	for b.Loop() {
		_ = languageIDOf(doc)
	}
}

func BenchmarkIsWholeDocument(b *testing.B) {
	changes := language.FullChange("x")
	for b.Loop() {
		_ = isWholeDocument(changes)
	}
}

func BenchmarkDecodeResult(b *testing.B) {
	raw := json.RawMessage(`{"contents":{"kind":"markdown","value":"func main()"}}`)
	var out map[string]any
	for b.Loop() {
		_ = decodeResult(raw, &out)
	}
}

func BenchmarkBackoff(b *testing.B) {
	for b.Loop() {
		_ = backoff(4)
	}
}

func BenchmarkLauncherOr(b *testing.B) {
	supplied := &fakeLauncher{}
	for b.Loop() {
		_ = launcherOr(supplied)
	}
}

func BenchmarkLifetimeOr(b *testing.B) {
	ctx := context.Background()
	for b.Loop() {
		_ = lifetimeOr(ctx)
	}
}

func BenchmarkDurationOr(b *testing.B) {
	for b.Loop() {
		_ = durationOr(0, defaultRequestTimeout)
	}
}

func BenchmarkRestartsOr(b *testing.B) {
	for b.Loop() {
		_ = restartsOr(0)
	}
}
