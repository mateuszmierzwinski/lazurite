package lsp

import (
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// note builds a publishDiagnostics notification from raw JSON params.
func note(params string) language.Notification {
	return language.Notification{
		Method: methodPublishDiagnostics, Params: []byte(params),
	}
}

// span is a well-formed zero-width range, so a case about something else does
// not have to restate one.
const span = `"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}`

// oneDiagnostic wraps a single diagnostic body in a publication for /w/a.go.
func oneDiagnostic(body string) language.Notification {
	return note(`{"uri":"file:///w/a.go","diagnostics":[{` + span + `,` + body + `}]}`)
}

func TestAPublicationIsDecodedIntoPathsAndDiagnostics(t *testing.T) {
	pub, ok := Published(note(`{"uri":"file:///w/main.go","version":7,"diagnostics":[` +
		`{"range":{"start":{"line":3,"character":4},"end":{"line":3,"character":9}},` +
		`"severity":2,"code":"SA1000","source":"staticcheck","message":"bad"}]}`))
	if !ok {
		t.Fatal("a well-formed publication was refused")
	}
	if pub.Path != "/w/main.go" || pub.Version != 7 {
		t.Fatalf("pub = %+v", pub)
	}
	d := pub.Diagnostics[0]
	if d.Severity != language.SeverityWarning || d.Code != "SA1000" || d.Source != "staticcheck" {
		t.Fatalf("diagnostic = %+v", d)
	}
	if d.Range.Start.Line != 3 || d.Range.End.Character != 9 {
		t.Fatalf("range = %+v", d.Range)
	}
}

func TestANumericCodeIsReadAndAnythingElseIsNot(t *testing.T) {
	// The protocol allows a number or a string. A struct that picked one would
	// silently drop every diagnostic from a server that chose the other.
	for name, c := range map[string]struct{ code, want string }{
		"a number":              {`1005`, "1005"},
		"a string":              {`"E501"`, "E501"},
		"an object it invented": {`{"value":1}`, ""},
		"an array":              {`[1]`, ""},
	} {
		pub, ok := Published(oneDiagnostic(`"severity":1,"code":` + c.code + `,"message":"x"`))
		if !ok || pub.Diagnostics[0].Code != c.want {
			t.Fatalf("%s: code = %q, want %q", name, pub.Diagnostics[0].Code, c.want)
		}
	}
}

func TestAMissingOrImpossibleSeverityIsReadAsAnError(t *testing.T) {
	// The protocol makes severity optional. A real problem shown as a hint is
	// one the operator never sees, so the safe direction is upward.
	for name, severity := range map[string]string{
		"absent":   `0`,
		"negative": `-4`,
		"invented": `99`,
	} {
		pub, _ := Published(oneDiagnostic(`"severity":` + severity + `,"message":"x"`))
		if got := pub.Diagnostics[0].Severity; got != language.SeverityError {
			t.Fatalf("%s: severity = %v, want error", name, got)
		}
	}
}

func TestANegativePositionIsBroughtBackIntoTheDocument(t *testing.T) {
	// A negative line is not a position, and one reaching a decoration layer
	// indexes behind the start of the buffer.
	pub, _ := Published(note(`{"uri":"file:///w/a.go","diagnostics":[` +
		`{"range":{"start":{"line":-5,"character":-2},"end":{"line":-1,"character":-1}},` +
		`"severity":1,"message":"x"}]}`))
	if got := pub.Diagnostics[0].Range.Start; got.Line != 0 || got.Character != 0 {
		t.Fatalf("start = %+v, want the document's own beginning", got)
	}
}

func TestADiagnosticMessageIsScrubbedBeforeItLeavesTheAdapter(t *testing.T) {
	pub, _ := Published(oneDiagnostic(`"severity":1,"message":"one\ntwo\u001b[2J"`))
	if got := pub.Diagnostics[0].Message; got != "one two[2J" {
		t.Fatalf("message = %q", got)
	}
}

func TestAFloodOfDiagnosticsIsBounded(t *testing.T) {
	// A server with a broken parser publishes one per token, and a pane that
	// listed a million of them is a window that stops responding.
	var body strings.Builder
	body.WriteString(`{"uri":"file:///w/a.go","diagnostics":[`)
	for i := 0; i < maxDiagnostics*3; i++ {
		if i > 0 {
			body.WriteString(",")
		}
		body.WriteString(`{` + span + `,"severity":1,"message":"x"}`)
	}
	body.WriteString(`]}`)
	pub, ok := Published(note(body.String()))
	if !ok {
		t.Fatal("a large publication was refused outright")
	}
	if len(pub.Diagnostics) != maxDiagnostics {
		t.Fatalf("kept %d diagnostics, want %d", len(pub.Diagnostics), maxDiagnostics)
	}
}

func TestNothingButAPublicationIsDecoded(t *testing.T) {
	for name, n := range map[string]language.Notification{
		"another method":           {Method: "window/logMessage", Params: []byte(`{}`)},
		"broken json":              note(`{"uri":`),
		"no uri":                   note(`{"diagnostics":[]}`),
		"a uri that is not a file": note(`{"uri":"http://example.com/a.go","diagnostics":[]}`),
	} {
		if _, ok := Published(n); ok {
			t.Fatalf("%s was decoded as a publication", name)
		}
	}
}

func BenchmarkPublished(b *testing.B) {
	n := note(`{"uri":"file:///w/main.go","diagnostics":[` +
		`{"range":{"start":{"line":3,"character":4},"end":{"line":3,"character":9}},` +
		`"severity":2,"message":"bad"}]}`)
	for b.Loop() {
		Published(n)
	}
}
