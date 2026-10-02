package lsp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// fullSyncCaps is a server that wants whole documents, which several real ones
// do.
const fullSyncCaps = `{"textDocumentSync":{"openClose":true,"change":1,"save":{"includeText":true}}}`

// noSyncCaps is a server that does not want document notifications at all.
const noSyncCaps = `{"textDocumentSync":0,"hoverProvider":true}`

// opened opens one document on the client, failing the test if it will not.
func opened(t *testing.T, h *harness, path string) {
	t.Helper()
	doc := language.Document{Path: path, Version: 1, Text: "package main"}
	if err := h.client.DidOpen(context.Background(), doc); err != nil {
		t.Fatalf("DidOpen = %v", err)
	}
}

func TestDidOpenTellsTheServerAboutTheBuffer(t *testing.T) {
	h := start(t, Deps{})
	opened(t, h, "/w/main.go")
	if !h.server.await(methodDidOpen, time.Second) {
		t.Fatal("no didOpen arrived; until it does the server knows nothing about the file")
	}
	params := paramsOf(t, h, methodDidOpen)
	for _, want := range []string{`"uri":"file:///w/main.go"`, `"languageId":"go"`, `"text":"package main"`} {
		if !strings.Contains(params, want) {
			t.Fatalf("didOpen params = %s, want %s in them", params, want)
		}
	}
}

func TestDidChangeCarriesTheEditAndAdvancesTheVersion(t *testing.T) {
	h := start(t, Deps{})
	opened(t, h, "/w/main.go")
	span := language.Range{Start: language.Position{Line: 0, Character: 8}, End: language.Position{Line: 0, Character: 12}}
	changes := []language.Change{{Range: &span, Text: "other"}}
	if err := h.client.DidChange(context.Background(), "/w/main.go", 2, changes); err != nil {
		t.Fatalf("DidChange = %v", err)
	}
	if !h.server.await(methodDidChange, time.Second) {
		t.Fatal("no didChange arrived")
	}
	params := paramsOf(t, h, methodDidChange)
	if !strings.Contains(params, `"version":2`) || !strings.Contains(params, `"character":12`) {
		t.Fatalf("didChange params = %s", params)
	}
	// The next edit must advance again, from the version just sent.
	if err := h.client.DidChange(context.Background(), "/w/main.go", 3, changes); err != nil {
		t.Fatalf("a second DidChange = %v", err)
	}
}

func TestDidChangeRefusesAVersionThatDidNotAdvance(t *testing.T) {
	// A version that repeats leaves the server choosing between two different
	// texts claiming to be the same one.
	h := start(t, Deps{})
	opened(t, h, "/w/main.go")
	for _, version := range []int{1, 0, -4} {
		err := h.client.DidChange(context.Background(), "/w/main.go", version, language.FullChange("x"))
		if !errors.Is(err, language.ErrStaleVersion) {
			t.Fatalf("DidChange at version %d = %v, want ErrStaleVersion", version, err)
		}
	}
}

func TestDidChangeRefusesADocumentThatWasNeverOpened(t *testing.T) {
	h := start(t, Deps{})
	err := h.client.DidChange(context.Background(), "/w/never.go", 2, language.FullChange("x"))
	if !errors.Is(err, language.ErrNotOpen) {
		t.Fatalf("DidChange = %v, want ErrNotOpen", err)
	}
}

func TestAWholeDocumentServerRefusesARangedChange(t *testing.T) {
	// This client keeps no copy of the operator's buffer to build a whole
	// document from, so the mismatch is refused by name rather than papered
	// over — the caller has the text and can send it.
	h := start(t, Deps{Launcher: &fakeLauncher{caps: fullSyncCaps}})
	opened(t, h, "/w/main.go")
	span := language.Range{}
	ranged := []language.Change{{Range: &span, Text: "part"}}
	err := h.client.DidChange(context.Background(), "/w/main.go", 2, ranged)
	if !errors.Is(err, language.ErrFullSyncRequired) {
		t.Fatalf("DidChange = %v, want ErrFullSyncRequired", err)
	}
	if err := h.client.DidChange(context.Background(), "/w/main.go", 2, language.FullChange("all")); err != nil {
		t.Fatalf("a whole-document change was refused too: %v", err)
	}
}

func TestDidSaveSendsTheTextOnlyWhereTheServerAskedForIt(t *testing.T) {
	wanting := start(t, Deps{Launcher: &fakeLauncher{caps: fullSyncCaps}})
	opened(t, wanting, "/w/main.go")
	if err := wanting.client.DidSave(context.Background(), "/w/main.go", "on disk"); err != nil {
		t.Fatalf("DidSave = %v", err)
	}
	if !wanting.server.await(methodDidSave, time.Second) {
		t.Fatal("no didSave arrived")
	}
	if !strings.Contains(paramsOf(t, wanting, methodDidSave), `"text":"on disk"`) {
		t.Fatal("the server asked for the text and did not get it")
	}

	quiet := start(t, Deps{})
	opened(t, quiet, "/w/main.go")
	if err := quiet.client.DidSave(context.Background(), "/w/main.go", "on disk"); err != nil {
		t.Fatalf("DidSave = %v", err)
	}
	if !quiet.server.await(methodDidSave, time.Second) {
		t.Fatal("no didSave arrived")
	}
	if strings.Contains(paramsOf(t, quiet, methodDidSave), "on disk") {
		t.Fatal("a megabyte was sent to a server that did not ask for it")
	}
}

func TestDidCloseForgetsTheDocument(t *testing.T) {
	h := start(t, Deps{})
	opened(t, h, "/w/main.go")
	if err := h.client.DidClose(context.Background(), "/w/main.go"); err != nil {
		t.Fatalf("DidClose = %v", err)
	}
	if !h.server.await(methodDidClose, time.Second) {
		t.Fatal("no didClose arrived")
	}
	if err := h.client.DidClose(context.Background(), "/w/main.go"); !errors.Is(err, language.ErrNotOpen) {
		t.Fatalf("a second DidClose = %v, want ErrNotOpen", err)
	}
	// The file can be opened again; a closed document is forgotten, not
	// blacklisted.
	opened(t, h, "/w/main.go")
}

func TestAServerThatWantsNoSynchronisationIsNotSentAny(t *testing.T) {
	h := start(t, Deps{Launcher: &fakeLauncher{caps: noSyncCaps}})
	doc := language.Document{Path: "/w/main.go", Version: 1, Text: "package main"}
	if err := h.client.DidOpen(context.Background(), doc); err != nil {
		t.Fatalf("DidOpen = %v", err)
	}
	if err := h.client.DidChange(context.Background(), "/w/main.go", 2, language.FullChange("x")); err != nil {
		t.Fatalf("DidChange = %v", err)
	}
	if err := h.client.DidSave(context.Background(), "/w/main.go", ""); err != nil {
		t.Fatalf("DidSave = %v", err)
	}
	if err := h.client.DidClose(context.Background(), "/w/main.go"); err != nil {
		t.Fatalf("DidClose = %v", err)
	}
	for _, method := range []string{methodDidOpen, methodDidChange, methodDidSave, methodDidClose} {
		if h.server.saw(method) {
			t.Fatalf("%s was sent to a server that wants no synchronisation", method)
		}
	}
}

func TestAnEmptyChangeSetIsNotSent(t *testing.T) {
	h := start(t, Deps{})
	opened(t, h, "/w/main.go")
	if err := h.client.DidChange(context.Background(), "/w/main.go", 2, nil); err != nil {
		t.Fatalf("DidChange = %v", err)
	}
	if h.server.saw(methodDidChange) {
		t.Fatal("an empty change set was sent")
	}
}

func TestARestartedServerIsToldAboutNoDocuments(t *testing.T) {
	// A new server is a new process with an empty view of the workspace.
	// Sending it a didChange for a file it was never told about desynchronises
	// it from the first keystroke, which surfaces later as diagnostics on lines
	// that no longer exist.
	h := start(t, Deps{MaxRestarts: 1})
	opened(t, h, "/w/main.go")
	h.server.crash()
	if !waitFor(5*time.Second, func() bool { return restarted(h) }) {
		t.Fatalf("status = %v", h.client.Status())
	}
	err := h.client.DidChange(context.Background(), "/w/main.go", 2, language.FullChange("x"))
	if !errors.Is(err, language.ErrNotOpen) {
		t.Fatalf("DidChange after a restart = %v, want ErrNotOpen", err)
	}
}

func TestSynchronisationRespectsACancelledContext(t *testing.T) {
	h := start(t, Deps{})
	opened(t, h, "/w/main.go")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, err := range map[string]error{
		"DidChange": h.client.DidChange(ctx, "/w/main.go", 2, language.FullChange("x")),
		"DidSave":   h.client.DidSave(ctx, "/w/main.go", ""),
	} {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s = %v, want Canceled", name, err)
		}
	}
}

// paramsOf is the parameters of the last message with this method.
func paramsOf(t *testing.T, h *harness, method string) string {
	t.Helper()
	for _, msg := range h.server.sent() {
		if msg.Method == method {
			return string(msg.Params)
		}
	}
	t.Fatalf("no %s was ever sent", method)
	return ""
}
