package lsp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// restarted says a replacement server is up.
//
// BOTH conditions, because the status is still Ready in the moment between a
// server dying and the reader noticing — a test waiting on the status alone
// passes instantly and then talks to the corpse.
func restarted(h *harness) bool {
	return h.launcher.started() > 1 && h.client.Status() == language.StatusReady
}

// countingReopener records that it was asked to re-announce the open documents.
type countingReopener struct {
	mu     sync.Mutex
	called int
	fail   error
}

// Reopen records the call and re-opens one document, as T5.2's will.
func (r *countingReopener) Reopen(ctx context.Context, client language.Client) error {
	r.mu.Lock()
	r.called++
	failure := r.fail
	r.mu.Unlock()
	if failure != nil {
		return failure
	}
	return client.DidOpen(ctx, language.Document{Path: "/w/main.go", Version: 1, Text: "package main"})
}

// times is how often Reopen was called.
func (r *countingReopener) times() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.called
}

func TestAServerThatNeverAnswersInitializeIsAServerThatIsNotThere(t *testing.T) {
	reporter := &recordingReporter{}
	client := New(Deps{
		Spec:             language.ServerSpec{Name: "fakels", Command: "fakels"},
		Launcher:         &fakeLauncher{caps: defaultCaps, silent: true},
		Reporter:         reporter,
		Lifetime:         context.Background(),
		HandshakeTimeout: 50 * time.Millisecond,
	})
	err := client.Start(context.Background())
	if !errors.Is(err, ErrHandshakeFailed) {
		t.Fatalf("Start = %v, want ErrHandshakeFailed", err)
	}
	if client.Status() != language.StatusFailed {
		t.Fatalf("status = %v, want failed", client.Status())
	}
	if !toldTheOperator(reporter, "no language features") {
		t.Fatalf("the operator was told %v", reporter.said())
	}
}

func TestACrashedServerIsRestartedAndTheDocumentsAreAskedFor(t *testing.T) {
	reopener := &countingReopener{}
	h := start(t, Deps{MaxRestarts: 1, Reopener: reopener})
	if err := h.client.DidOpen(context.Background(), language.Document{Path: "/w/main.go", Version: 1}); err != nil {
		t.Fatalf("DidOpen = %v", err)
	}
	h.server.crash()
	if !waitFor(5*time.Second, func() bool { return h.launcher.started() == 2 }) {
		t.Fatalf("the server was launched %d times; a crash must be restarted", h.launcher.started())
	}
	if !waitFor(5*time.Second, func() bool { return h.client.Status() == language.StatusReady }) {
		t.Fatalf("status after the restart = %v", h.client.Status())
	}
	if reopener.times() != 1 {
		t.Fatalf("Reopen was called %d times; a new server knows about no files", reopener.times())
	}
	// The replacement really is the one being talked to now.
	h.launcher.latest().results["textDocument/hover"] = `{"contents":"from the new server"}`
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.client.Request(ctx, "textDocument/hover", nil, nil); err != nil {
		t.Fatalf("Request against the restarted server = %v", err)
	}
}

func TestARestartThatKeepsFailingGivesUpRatherThanLoopingForever(t *testing.T) {
	launcher := &fakeLauncher{caps: defaultCaps}
	h := start(t, Deps{Launcher: launcher, MaxRestarts: 1})
	launcher.mu.Lock()
	launcher.failure = errors.New("executable file not found in $PATH")
	launcher.mu.Unlock()
	h.server.crash()
	if !waitFor(10*time.Second, func() bool { return h.client.Status() == language.StatusFailed }) {
		t.Fatalf("status = %v, want failed after the budget ran out", h.client.Status())
	}
	if !waitFor(2*time.Second, func() bool { return toldTheOperator(h.reporter, "no language features") }) {
		t.Fatalf("the operator was told %v", h.reporter.said())
	}
	// Terminal: nothing keeps relaunching in the background.
	before := launcher.started()
	time.Sleep(300 * time.Millisecond)
	if launcher.started() != before {
		t.Fatalf("launches went from %d to %d after giving up", before, launcher.started())
	}
}

func TestACallDuringTheBackoffIsRefusedRatherThanWrittenToADeadPipe(t *testing.T) {
	h := start(t, Deps{MaxRestarts: 1})
	h.server.crash()
	if !waitFor(2*time.Second, func() bool { return h.client.Status() != language.StatusReady }) {
		t.Fatalf("status = %v; the dead session was still being offered", h.client.Status())
	}
	err := h.client.Request(context.Background(), "textDocument/hover", nil, nil)
	if !errors.Is(err, language.ErrNotRunning) {
		t.Fatalf("Request = %v, want ErrNotRunning", err)
	}
}

func TestStopSaysGoodbyeInTheProtocolsOrder(t *testing.T) {
	h := start(t, Deps{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.client.Stop(ctx); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	if !waitFor(2*time.Second, func() bool { return len(farewell(h)) == 2 }) {
		t.Fatalf("the farewell was %v, want shutdown then exit", farewell(h))
	}
	if said := farewell(h); said[0] != methodShutdown || said[1] != methodExit {
		t.Fatalf("the farewell was %v, want shutdown then exit", said)
	}
	if h.client.Status() != language.StatusStopped {
		t.Fatalf("status = %v, want stopped", h.client.Status())
	}
}

func TestStopIsIdempotentAndTerminal(t *testing.T) {
	h := start(t, Deps{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.client.Stop(ctx); err != nil {
		t.Fatalf("first Stop = %v", err)
	}
	if err := h.client.Stop(ctx); err != nil {
		t.Fatalf("second Stop = %v", err)
	}
	launches := h.launcher.started()
	if err := h.client.Start(context.Background()); err != nil {
		t.Fatalf("Start after Stop = %v, want a quiet refusal", err)
	}
	if h.launcher.started() != launches {
		t.Fatal("a stopped client started a new server; Stop is terminal")
	}
	if h.client.Status() != language.StatusStopped {
		t.Fatalf("status = %v", h.client.Status())
	}
}

func TestAClosingWindowStopsTheServerAndTheSupervisor(t *testing.T) {
	// The lifetime is the WINDOW's context, threaded from the composition root.
	// Nothing this package starts may outlive it.
	launcher := &fakeLauncher{caps: defaultCaps}
	ctx, cancel := context.WithCancel(context.Background())
	client := New(Deps{
		Spec:     language.ServerSpec{Name: "fakels", Command: "fakels"},
		Launcher: launcher, Lifetime: ctx, MaxRestarts: 5,
	})
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	cancel()
	launcher.latest().crash()
	before := launcher.started()
	time.Sleep(800 * time.Millisecond)
	if launcher.started() != before {
		t.Fatalf("launches went from %d to %d after the window closed", before, launcher.started())
	}
}

func TestAFailingReopenerIsReportedRatherThanFatal(t *testing.T) {
	reopener := &countingReopener{fail: errors.New("the buffers are locked")}
	h := start(t, Deps{MaxRestarts: 1, Reopener: reopener})
	h.server.crash()
	if !waitFor(5*time.Second, func() bool { return restarted(h) }) {
		t.Fatalf("status = %v; a failing reopener must not stop the restart", h.client.Status())
	}
	if !waitFor(2*time.Second, func() bool { return toldTheOperator(h.reporter, "re-open documents") }) {
		t.Fatalf("the operator was told %v", h.reporter.said())
	}
}

// farewell is the lifecycle methods the client sent, in order.
func farewell(h *harness) []string {
	said := []string{}
	for _, msg := range h.server.sent() {
		if msg.Method == methodShutdown || msg.Method == methodExit {
			said = append(said, msg.Method)
		}
	}
	return said
}
