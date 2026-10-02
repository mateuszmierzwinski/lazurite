package lsp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// harness is a started client and the fake server behind it.
type harness struct {
	client   language.Client
	launcher *fakeLauncher
	server   *fakeServer
	sink     *recordingSink
	reporter *recordingReporter
}

// start builds a client over a fake server and completes the handshake.
//
// The Deps it is given are the caller's, with the pieces every test needs
// filled in — so a test says only what makes it different from the others.
func start(t *testing.T, deps Deps) *harness {
	t.Helper()
	launcher, _ := deps.Launcher.(*fakeLauncher)
	if launcher == nil {
		launcher = &fakeLauncher{caps: defaultCaps}
		deps.Launcher = launcher
	}
	sink, reporter := newRecordingSink(), &recordingReporter{}
	deps.Sink, deps.Reporter = sink, reporter
	deps.Spec = specOr(deps.Spec)
	ctx, cancel := context.WithCancel(context.Background())
	deps.Lifetime = ctx
	client := New(deps)
	t.Cleanup(func() {
		_ = client.Stop(context.Background())
		cancel()
	})
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	return &harness{client, launcher, launcher.latest(), sink, reporter}
}

// specOr fills in a workable spec where a test did not name one.
func specOr(spec language.ServerSpec) language.ServerSpec {
	if spec.Command == "" {
		spec = language.ServerSpec{Name: "fakels", Command: "fakels", Root: "/w", Extensions: []string{".go"}}
	}
	return spec
}

func TestTheHandshakeReadsTheServersCapabilities(t *testing.T) {
	h := start(t, Deps{})
	if h.client.Status() != language.StatusReady {
		t.Fatalf("status = %v, want ready", h.client.Status())
	}
	caps := h.client.Capabilities()
	if !caps.Hover || !caps.Definition || caps.Sync != language.SyncIncremental {
		t.Fatalf("capabilities = %+v", caps)
	}
	// A consumer asks rather than calling and failing, so a capability the
	// server did NOT offer must read false.
	if caps.CallHierarchy || caps.WorkspaceSymbol {
		t.Fatalf("capabilities claimed something the server never offered: %+v", caps)
	}
	if !h.server.await(methodInitialized, time.Second) {
		t.Fatal("the client never sent `initialized`; a server waits for it before indexing")
	}
}

func TestARequestGetsItsAnswer(t *testing.T) {
	h := start(t, Deps{})
	h.server.results["textDocument/hover"] = `{"contents":{"kind":"markdown","value":"func main()"}}`
	var out struct {
		Contents struct {
			Value string `json:"value"`
		} `json:"contents"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.client.Request(ctx, "textDocument/hover", nil, &out); err != nil {
		t.Fatalf("Request = %v", err)
	}
	if out.Contents.Value != "func main()" {
		t.Fatalf("hover = %q", out.Contents.Value)
	}
}

func TestAServerErrorReachesTheCallerWithItsCode(t *testing.T) {
	h := start(t, Deps{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := h.client.Request(ctx, "textDocument/inlayHint", nil, nil)
	var remote *remoteError
	if !errors.As(err, &remote) {
		t.Fatalf("Request = %v, want a remoteError", err)
	}
	if remote.Code != codeMethodNotFound {
		t.Fatalf("code = %d, want %d", remote.Code, codeMethodNotFound)
	}
}

func TestARequestWithNoDeadlineOfItsOwnGetsOne(t *testing.T) {
	// lazurite's own rule: a call with no deadline of its own gets one here.
	// "The library will time out" is a claim to check, and a wedged server is
	// not a hypothetical — it is what a language server does under load.
	h := start(t, Deps{RequestTimeout: 40 * time.Millisecond})
	h.server.ignore["textDocument/hover"] = true
	began := time.Now()
	err := h.client.Request(context.Background(), "textDocument/hover", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Request = %v, want DeadlineExceeded", err)
	}
	// Loose on purpose: the claim is that the bound EXISTS, not that the
	// scheduler is prompt. Forty milliseconds against five seconds proves it
	// applied without failing on a loaded build machine.
	if elapsed := time.Since(began); elapsed > 5*time.Second {
		t.Fatalf("the request took %v; the client's own bound did not apply", elapsed)
	}
}

func TestACancelledRequestIsWithdrawnFromTheServer(t *testing.T) {
	// A completion the operator has already typed past must be DROPPED rather
	// than applied late over what they are writing now.
	h := start(t, Deps{})
	h.server.ignore["textDocument/completion"] = true
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if err := h.client.Request(ctx, "textDocument/completion", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Request = %v, want Canceled", err)
	}
	if !h.server.await(methodCancelRequest, time.Second) {
		t.Fatal("no $/cancelRequest was sent; the server keeps working on an answer nobody wants")
	}
}

func TestALateAnswerToACancelledRequestIsDropped(t *testing.T) {
	h := start(t, Deps{RequestTimeout: 30 * time.Millisecond})
	h.server.ignore["textDocument/completion"] = true
	if err := h.client.Request(context.Background(), "textDocument/completion", nil, nil); err == nil {
		t.Fatal("the request was answered; this test needs it to time out")
	}
	// The server answers anyway, as a real one does when it finishes work it
	// was asked to abandon. Nothing is waiting, and the session must survive.
	h.server.respond([]byte("2"), `{"items":[]}`)
	h.server.results["textDocument/hover"] = `{"contents":"still here"}`
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.client.Request(ctx, "textDocument/hover", nil, nil); err != nil {
		t.Fatalf("a stale answer broke the session: %v", err)
	}
}

func TestANotificationReachesTheSink(t *testing.T) {
	h := start(t, Deps{})
	h.server.notify("textDocument/publishDiagnostics", `{"uri":"file:///w/main.go","diagnostics":[]}`)
	if !waitFor(time.Second, func() bool { return len(h.sink.notifications()) > 0 }) {
		t.Fatal("nothing reached the sink; diagnostics arrive this way and no other")
	}
	got := h.sink.notifications()[0]
	if got.Method != "textDocument/publishDiagnostics" {
		t.Fatalf("method = %q", got.Method)
	}
	if !strings.Contains(string(got.Params), "file:///w/main.go") {
		t.Fatalf("params = %s; they must reach the consumer undecoded", got.Params)
	}
}

func TestAServerRequestIsAlwaysAnswered(t *testing.T) {
	// A server left waiting on an answer stalls whatever it was doing on the
	// editor's behalf, so even a method this client does not implement is
	// answered rather than ignored.
	h := start(t, Deps{})
	h.server.ask(41, "client/registerCapability", `{"registrations":[]}`)
	h.server.ask(42, "workspace/configuration", `{"items":[{"section":"gopls"},{"section":"go"}]}`)
	h.server.ask(43, "telemetry/somethingWeNeverHeardOf", `{}`)
	h.server.ask(44, "workspace/applyEdit", `{"edit":{}}`)
	for id, want := range map[string]string{
		"41": "null",
		"42": "[{},{}]",
		"44": `"applied":false`,
	} {
		reply, arrived := h.server.awaitReply(id, 2*time.Second)
		if !arrived {
			t.Fatalf("request %s was never answered", id)
		}
		if !strings.Contains(string(reply.Result), want) {
			t.Fatalf("request %s answered %s, want %s in it", id, reply.Result, want)
		}
	}
	unknown, arrived := h.server.awaitReply("43", 2*time.Second)
	if !arrived || unknown.Error == nil || unknown.Error.Code != codeMethodNotFound {
		t.Fatalf("an unimplemented method was answered %+v, want method-not-found", unknown)
	}
}

func TestAMessageLargerThanTheCapEndsTheSessionRatherThanTheProcess(t *testing.T) {
	// A hostile or broken server announcing four gigabytes must cost this
	// process nothing. The stream cannot be resynchronised after that, so the
	// session dies and the editor degrades — which is the promise.
	h := start(t, Deps{MaxRestarts: -1})
	h.server.raw("Content-Length: 4294967296\r\n\r\n")
	if !waitFor(2*time.Second, func() bool { return h.client.Status() == language.StatusFailed }) {
		t.Fatalf("status = %v, want failed", h.client.Status())
	}
	if err := h.client.Request(context.Background(), "textDocument/hover", nil, nil); !errors.Is(err, language.ErrNotRunning) {
		t.Fatalf("Request after the session died = %v, want ErrNotRunning", err)
	}
	if !waitFor(2*time.Second, func() bool { return toldTheOperator(h.reporter, "no language features") }) {
		t.Fatalf("the operator was told %v", h.reporter.said())
	}
}

func TestAMessageThatIsNotJSONIsSurvivedRatherThanFatal(t *testing.T) {
	// The frame was well-formed, so the stream is still in step and the next
	// message starts where it should. Only a FRAMING fault is unrecoverable.
	h := start(t, Deps{})
	h.server.write(`this is not json at all`)
	h.server.results["textDocument/hover"] = `{"contents":"still here"}`
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.client.Request(ctx, "textDocument/hover", nil, nil); err != nil {
		t.Fatalf("a single unreadable message took the session down: %v", err)
	}
}

func TestNothingIsSentToAClientThatIsNotRunning(t *testing.T) {
	client := New(Deps{
		Spec:     language.ServerSpec{Command: "fakels"},
		Launcher: &fakeLauncher{failure: errors.New("executable file not found in $PATH")},
		Lifetime: context.Background(),
	})
	if err := client.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded with no server to start")
	}
	if client.Status() != language.StatusFailed {
		t.Fatalf("status = %v, want failed", client.Status())
	}
	if client.Capabilities().Hover {
		t.Fatal("a client that never started advertised a capability")
	}
	for name, call := range map[string]error{
		"Request": client.Request(context.Background(), "textDocument/hover", nil, nil),
		"Notify":  client.Notify(context.Background(), "$/setTrace", nil),
		"DidOpen": client.DidOpen(context.Background(), language.Document{Path: "/w/main.go"}),
	} {
		if !errors.Is(call, language.ErrNotRunning) {
			t.Fatalf("%s = %v, want ErrNotRunning", name, call)
		}
	}
	// Stopping something that never started is not an error.
	if err := client.Stop(context.Background()); err != nil {
		t.Fatalf("Stop = %v", err)
	}
}

func TestAClientWithNoCommandRefusesToStart(t *testing.T) {
	client := New(Deps{Lifetime: context.Background(), Launcher: &fakeLauncher{}})
	if err := client.Start(context.Background()); !errors.Is(err, ErrNotAServerCommand) {
		t.Fatalf("Start = %v, want ErrNotAServerCommand", err)
	}
}

// toldTheOperator says whether any reported message carried this phrase.
func toldTheOperator(reporter *recordingReporter, phrase string) bool {
	for _, message := range reporter.said() {
		if strings.Contains(message, phrase) {
			return true
		}
	}
	return false
}
