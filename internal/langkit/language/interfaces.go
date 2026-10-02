// Package language is lazurite's vocabulary for language intelligence, and the
// ports the editor consumes it through.
//
// It knows NOTHING about the Language Server Protocol's wire format, about
// JSON-RPC or about subprocesses. `internal/adapters/lsp` is one implementation
// of Client; a test supplies another, and `Absent` is the third — the one that
// answers "there is no language server" without anything above it having to ask
// whether there is one.
//
// # What a consumer must call (T5.2, T5.3, T5.4)
//
// The window never holds a Client directly. It holds a Servers router, asks it
// for the client that handles a path, and gets a working Client either way:
//
//	client := servers.For(path)              // never nil; Absent() when none
//	if !client.Capabilities().Hover { ... }  // ASK, do not call and fail
//	err := client.Request(ctx, "textDocument/hover", params, &result)
//
// Three rules the consumer owns and this package cannot enforce:
//
//   - Every call takes a context with a DEADLINE. A language server is a
//     subprocess that can stop answering; a request with no deadline is a hang.
//   - Nothing here may run on the frame thread, and nothing here may touch a
//     widget. Call from a goroutine, hand the answer back through `Shell.Post`.
//   - A Sink's Publish is called from the adapter's reader goroutine. It must
//     not block and it must not touch a widget — post, and return.
package language

import "context"

// Client is one language server, seen from the editor.
//
// A PORT. Its implementations are the LSP adapter, a test double, and Absent —
// which satisfies the whole interface by refusing everything, so a window with
// no server running is a window with no language features rather than a window
// full of nil checks.
//
// Request and Notify carry a METHOD NAME rather than one method per LSP
// operation, and that is a deliberate trade: the alternative is thirty methods
// on this interface before a single one of them is consumed, decided by T5.1
// on behalf of three tasks that have not been written yet. The transport is
// what T5.1 owes them; the vocabulary of hover, rename and formatting is theirs
// to add here as they need it.
type Client interface {
	// Start brings the server up and completes the initialize handshake.
	//
	// The context bounds the HANDSHAKE, not the server's life — a server that
	// never answers initialize is a server that is not there. It returns an
	// error rather than degrading silently, and the client is left in
	// StatusFailed, where every other call answers ErrNotRunning.
	Start(ctx context.Context) error

	// Status says whether language features are available right now.
	Status() Status

	// Capabilities is what the server said it can do, so a consumer asks
	// rather than calling and failing. It is the zero value until the
	// handshake completes, and the zero value supports nothing.
	Capabilities() Capabilities

	// Request sends one JSON-RPC request and decodes the result into out,
	// which may be nil where the answer is not wanted.
	//
	// It returns when the server answers, when ctx is done — in which case a
	// $/cancelRequest is sent and a late answer is DROPPED rather than applied
	// — or when the server goes away.
	Request(ctx context.Context, method string, params, out any) error

	// Notify sends one JSON-RPC notification, which has no answer.
	Notify(ctx context.Context, method string, params any) error

	// DidOpen tells the server a document is being edited, and hands over its
	// current text. Until this is called the server knows nothing about the
	// file, whatever is on disk.
	DidOpen(ctx context.Context, doc Document) error

	// DidChange reports edits to an already-open document.
	//
	// Version must increase. Where Capabilities().Sync is SyncFull the changes
	// must be a single whole-text Change (see FullChange) — this client keeps
	// no second copy of the operator's buffer, because a second copy that
	// drifts is worse than asking the caller for text it already has.
	DidChange(ctx context.Context, path string, version int, changes []Change) error

	// DidSave tells the server the document reached the disk. Text may be
	// empty where the server did not ask to be sent it.
	DidSave(ctx context.Context, path, text string) error

	// DidClose tells the server to forget a document. What is on disk is
	// authoritative again from here.
	DidClose(ctx context.Context, path string) error

	// Stop performs the shutdown/exit handshake and ends the process.
	//
	// It is idempotent, and a client that never started stops cleanly. The
	// context bounds how long a polite exit is waited for; past it the process
	// is killed, because a server refusing to leave must not outlive the
	// window.
	Stop(ctx context.Context) error
}

// Sink receives what the server says without being asked.
//
// Diagnostics, log messages and progress all arrive this way. Params is the
// raw JSON of the notification, undecoded: this package does not know what
// `textDocument/publishDiagnostics` looks like on the wire, and T5.2 does.
//
// Called from the adapter's READER GOROUTINE. It must not block and must not
// touch a widget.
type Sink interface {
	Publish(n Notification)
}

// Reporter is how a degradation reaches the operator.
//
// A server that is missing, crashed or gave up restarting is not an error
// anybody asked for — nobody pressed anything — so it is reported rather than
// returned. Called from a background goroutine, with the same two rules a Sink
// has.
type Reporter interface {
	Report(message string)
}

// Reopener re-announces the editor's open documents to a server that has just
// been restarted.
//
// A restarted server knows NOTHING: it is a new process with an empty view of
// the workspace, and every buffer the operator has open is a file it has never
// been told about. Until each is opened again there are no diagnostics, no
// hover and no completion for any of them — which reads as the language server
// having quietly stopped working, because from the operator's side it has.
//
// The client cannot do this itself, and deliberately does not try: it would
// need its own copy of every buffer's text, and a second copy that drifts from
// the editor's is worse than not having one (see Client.DidChange). The
// consumer owns the buffers, so the consumer is asked.
//
// T5.2 implements this. Called from the client's supervisor goroutine, so the
// same two rules apply: do not block for long, and do not touch a widget —
// read the buffers through whatever the editor's own state is guarded by, and
// call DidOpen on the client handed in.
type Reopener interface {
	Reopen(ctx context.Context, client Client) error
}
