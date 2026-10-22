package gopls

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
)

// fakeClient is a language server that answers what a test tells it to.
type fakeClient struct {
	mu      sync.Mutex
	log     []string
	answers map[string]string
	params  map[string]string
	failing error
}

func (f *fakeClient) note(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, s)
}

func (f *fakeClient) said() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.log, ",")
}

func (f *fakeClient) Start(context.Context) error { f.note("start"); return nil }
func (f *fakeClient) Status() language.Status     { return language.StatusStopped }
func (f *fakeClient) Capabilities() language.Capabilities {
	return language.Capabilities{Completion: true, Definition: true, Hover: true}
}
func (f *fakeClient) Request(_ context.Context, method string, params, out any) error {
	sent, _ := json.Marshal(params)
	f.mu.Lock()
	f.params[method] = string(sent)
	answer := f.answers[method]
	f.mu.Unlock()
	if f.failing != nil {
		return f.failing
	}
	return json.Unmarshal([]byte(answer), out)
}
func (f *fakeClient) Notify(context.Context, string, any) error { return nil }
func (f *fakeClient) DidOpen(_ context.Context, d language.Document) error {
	f.note("open " + filepath.Base(d.Path) + "@" + string(rune('0'+d.Version)))
	return nil
}
func (f *fakeClient) DidChange(_ context.Context, path string, version int, _ []language.Change) error {
	f.note("change " + filepath.Base(path) + "@" + string(rune('0'+version)))
	return nil
}
func (f *fakeClient) DidSave(_ context.Context, path, _ string) error {
	f.note("save " + filepath.Base(path))
	return nil
}
func (f *fakeClient) DidClose(_ context.Context, path string) error {
	f.note("close " + filepath.Base(path))
	return nil
}
func (f *fakeClient) Stop(context.Context) error { f.note("stop"); return nil }

// server is a Server over a fake client.
func server(t *testing.T) (*Server, *fakeClient) {
	t.Helper()
	fake := &fakeClient{answers: map[string]string{}, params: map[string]string{}}
	s := start(context.Background(), func(*Server) language.Client { return fake })
	t.Cleanup(s.Stop)
	return s, fake
}

// settle waits until the worker has done everything queued so far.
func settle(s *Server) {
	done := make(chan struct{})
	s.do(func() { close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

const emojiLine = "package x\n\tname := \"😀\" + y\n"

func TestAFilesLifeIsToldToTheServerInOrderAndVersioned(t *testing.T) {
	s, fake := server(t)
	s.Open("/p/a.go", "one")
	s.Change("/p/a.go", "two")
	s.Change("/p/a.go", "two")
	s.Save("/p/a.go", "two")
	s.Close("/p/a.go")
	settle(s)
	if got := fake.said(); got != "start,open a.go@1,change a.go@2,save a.go,close a.go" {
		t.Fatalf("the server was told: %s", got)
	}
}

func TestCompletionsAreAskedAtTheProtocolsColumnAndComeBackAsTheEditors(t *testing.T) {
	s, fake := server(t)
	s.Open("/p/a.go", emojiLine)
	fake.answers["textDocument/completion"] = `{"isIncomplete":false,"items":[
		{"label":"zeta","kind":6,"sortText":"00002"},
		{"label":"ToUpper","kind":3,"detail":"func(s string) string","sortText":"00001","insertText":"ToUpper",
		 "additionalTextEdits":[{"range":{"start":{"line":1,"character":11},"end":{"line":1,"character":13}},"newText":"X"}]}]}`
	got := make(chan []ide.Suggestion, 1)
	s.Complete("/p/a.go", edit.Pos{Line: 1, Col: 13}, func(items []ide.Suggestion) { got <- items })
	items := <-got
	if !strings.Contains(fake.params["textDocument/completion"], `"character":14`) {
		t.Fatalf("asked at %s — an emoji is two units of the protocol's counting", fake.params["textDocument/completion"])
	}
	if len(items) != 2 || items[0].Label != "ToUpper" || items[0].Detail != "func(s string) string" || items[1].Detail != "var" || items[1].Insert != "zeta" {
		t.Fatalf("got %+v", items)
	}
	if e := items[0].Extra; len(e) != 1 || e[0].From != (edit.Pos{Line: 1, Col: 11}) || e[0].To != (edit.Pos{Line: 1, Col: 12}) {
		t.Fatalf("the extra edit is at %+v — the emoji is one character of the editor's counting", items[0].Extra)
	}
}

func TestADefinitionAndADescriptionAreAsked(t *testing.T) {
	s, fake := server(t)
	s.Open("/p/a.go", emojiLine)
	fake.answers["textDocument/definition"] = `[{"uri":"file:///p/other.go","range":{"start":{"line":4,"character":5},"end":{"line":4,"character":9}}}]`
	fake.answers["textDocument/hover"] = `{"contents":{"kind":"plaintext","value":"func y() string"}}`
	place := make(chan ide.Place, 1)
	s.Define("/p/a.go", edit.Pos{Line: 1, Col: 3}, func(p ide.Place, ok bool) {
		if !ok {
			t.Error("no definition")
		}
		place <- p
	})
	if got := <-place; got != (ide.Place{Path: "/p/other.go", At: edit.Pos{Line: 4, Col: 5}}) {
		t.Fatalf("got %+v", got)
	}
	about := make(chan string, 1)
	s.Describe("/p/a.go", edit.Pos{Line: 1, Col: 3}, func(text string) { about <- text })
	if got := <-about; !strings.Contains(got, "func y() string") {
		t.Fatalf("got %q", got)
	}
}

func TestAServerThatFailsAnswersNothing(t *testing.T) {
	s, fake := server(t)
	fake.failing = errors.New("server crashed")
	items := make(chan []ide.Suggestion, 1)
	s.Complete("/p/a.go", edit.Pos{}, func(got []ide.Suggestion) { items <- got })
	if got := <-items; got != nil {
		t.Fatalf("got %+v", got)
	}
	found := make(chan bool, 1)
	s.Define("/p/a.go", edit.Pos{}, func(_ ide.Place, ok bool) { found <- ok })
	if <-found {
		t.Fatal("a definition from a failed server")
	}
	about := make(chan string, 1)
	s.Describe("/p/a.go", edit.Pos{}, func(text string) { about <- text })
	if <-about != "" {
		t.Fatal("a description from a failed server")
	}
}

func TestWhatIsWrongIsPassedOnInTheEditorsColumns(t *testing.T) {
	s, _ := server(t)
	s.Open("/p/a.go", emojiLine)
	settle(s)
	type heard struct {
		path     string
		problems []ide.Problem
	}
	got := make(chan heard, 1)
	s.Listen(func(path string, problems []ide.Problem) { got <- heard{path, problems} })
	s.Publish(language.Notification{Method: "textDocument/publishDiagnostics", Params: []byte(`{"uri":"file:///p/a.go","diagnostics":[
		{"range":{"start":{"line":1,"character":16},"end":{"line":1,"character":17}},"severity":1,"message":"undefined: y"},
		{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"severity":2,"message":"odd"},
		{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"severity":4,"message":"a hint"}]}`)})
	h := <-got
	if h.path != "/p/a.go" || len(h.problems) != 2 {
		t.Fatalf("got %+v", h)
	}
	if p := h.problems[0]; !p.Severe || p.From != (edit.Pos{Line: 1, Col: 15}) || p.To != (edit.Pos{Line: 1, Col: 16}) || p.Message != "undefined: y" {
		t.Fatalf("got %+v", p)
	}
	if p := h.problems[1]; p.Severe || p.To.Col != 1 {
		t.Fatalf("an empty range was not widened to one character: %+v", p)
	}
	s.Publish(language.Notification{Method: "window/logMessage", Params: []byte(`{}`)})
	s.Listen(nil)
	s.Publish(language.Notification{Method: "textDocument/publishDiagnostics", Params: []byte(`{"uri":"file:///p/a.go","diagnostics":[]}`)})
	s.Report("the server said something")
}

func TestARestartedServerIsToldOfTheOpenFilesAgain(t *testing.T) {
	s, fake := server(t)
	s.Open("/p/b.go", "b")
	s.Open("/p/a.go", "a")
	settle(s)
	if err := s.Reopen(context.Background(), fake); err != nil {
		t.Fatal(err)
	}
	if got := fake.said(); !strings.HasSuffix(got, "open a.go@2,open b.go@2") {
		t.Fatalf("the server was told: %s", got)
	}
}

func TestAfterStopNothingIsAskedAndNothingBlocks(t *testing.T) {
	fake := &fakeClient{answers: map[string]string{}, params: map[string]string{}}
	s := start(context.Background(), func(*Server) language.Client { return fake })
	s.Stop()
	for i := 0; i < 1000; i++ {
		s.Open("/p/a.go", "x")
	}
	if strings.Contains(fake.said(), "open") {
		t.Fatal("a stopped server was told of a file")
	}
}

func TestTheRealConstructorWorksWithoutAServerToStart(t *testing.T) {
	intel := New(context.Background(), filepath.Join(t.TempDir(), "no-such-gopls"), t.TempDir())
	done := make(chan []ide.Suggestion, 1)
	intel.Complete("/p/a.go", edit.Pos{}, func(items []ide.Suggestion) { done <- items })
	select {
	case got := <-done:
		if got != nil {
			t.Fatalf("got %+v", got)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("a server that cannot start left a question unanswered")
	}
	intel.Stop()
}

func TestPositionsAreConvertedBothWays(t *testing.T) {
	text := "ab\n😀x😀y\nlast"
	for at, units := range map[edit.Pos]int{{Line: 1, Col: 0}: 0, {Line: 1, Col: 1}: 2, {Line: 1, Col: 2}: 3, {Line: 1, Col: 3}: 5, {Line: 1, Col: 99}: 6, {Line: 9, Col: 3}: 0} {
		if got := position(text, at); got.Character != units || got.Line != at.Line {
			t.Errorf("position(%+v) = %+v, want %d units", at, got, units)
		}
	}
	for units, col := range map[int]int{0: 0, 2: 1, 3: 2, 5: 3, 99: 4} {
		if got := caret(text, language.Position{Line: 1, Character: units}); got.Col != col {
			t.Errorf("caret(%d units) = col %d, want %d", units, got.Col, col)
		}
	}
	if got := caret("", language.Position{Line: -1, Character: 7}); got != (edit.Pos{Col: 7}) {
		t.Fatalf("with no text the column is taken as it is, inside the text: %+v", got)
	}
	if lineOf(text, 2) != "last" || lineOf(text, 3) != "" || lineOf(text, 0) != "ab" {
		t.Fatal("lineOf")
	}
}

func BenchmarkPosition(b *testing.B) {
	text := strings.Repeat("some line of code\n", 400)
	for b.Loop() {
		position(text, edit.Pos{Line: 300, Col: 10})
	}
}

func BenchmarkCaret(b *testing.B) {
	text := strings.Repeat("some line of code\n", 400)
	for b.Loop() {
		caret(text, language.Position{Line: 300, Character: 10})
	}
}

func BenchmarkLineOf(b *testing.B) {
	text := strings.Repeat("some line of code\n", 400)
	for b.Loop() {
		lineOf(text, 300)
	}
}

func BenchmarkUnitsOf(b *testing.B) {
	for b.Loop() {
		unitsOf('😀')
	}
}

func BenchmarkProblems(b *testing.B) {
	found := []language.Diagnostic{{Severity: language.SeverityError, Message: "x"}, {Severity: language.SeverityHint}}
	for b.Loop() {
		problems("package x\n", found)
	}
}

func BenchmarkSuggestions(b *testing.B) {
	items := []language.Completion{{Label: "b", Sort: "2"}, {Label: "a", Sort: "1"}}
	for b.Loop() {
		suggestions("package x\n", items)
	}
}

func BenchmarkSuggestion(b *testing.B) {
	item := language.Completion{Label: "Println", Kind: language.KindFunction}
	for b.Loop() {
		suggestion("package x\n", item)
	}
}
