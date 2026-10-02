// Package lsp speaks the Language Server Protocol to a server running as a
// child process, over stdio.
//
// It implements `core/language.Client` and nothing above it needs to know that
// the protocol exists. What it owes T5.2 to T5.4 is the transport and the
// lifecycle: framing, the request map, document synchronisation, the initialize
// handshake, and a server that is missing or falling over degrading to no
// language features rather than to a broken editor.
//
// # What is trusted
//
// The server BINARY is trusted the way any program the user runs is: it is
// named by the user's configuration or found on their PATH, it is started with
// their privileges, and it can read and write whatever they can. Choosing which
// binary to run is the trust boundary, and this package does not try to be a
// sandbox — see `test/cve/cve_lsp_test.go`.
//
// What is NOT trusted is anything that comes back down the pipe. A server that
// is broken, wedged or hostile must not be able to exhaust this process's
// memory, desynchronise its message stream, hang a caller, or steer the editor
// into a file outside the workspace. Those are the bounds in `codec.go`, the
// deadlines in `client.go`, and `language.Confined` respectively.
//
// # Goroutines
//
// Two per client, both owned here and both documented at their definitions:
// `session.read` (one per live server process) and `client.supervise` (one for
// the client's life). Neither ever touches a widget; answers reach the window
// through the ports in `core/language` and the window's own `Shell.Post`.
package lsp

import (
	"context"
	"io"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// Transport is the byte stream to one running server.
//
// A PORT, and the reason no test in this package starts a real language server.
// The process implementation is `process.go`; a test supplies a pair of pipes
// with an in-process fake on the other end, which speaks exactly the same
// protocol over exactly the same framing.
type Transport interface {
	// Read and Write are the server's stdout and stdin. A Read that returns an
	// error is how the reader goroutine learns the server is gone, so an
	// implementation must NOT swallow EOF.
	io.Reader
	io.Writer

	// Close ends the stream and the process behind it, and must unblock a Read
	// that is in progress — that is what terminates the reader goroutine.
	//
	// It is bounded internally rather than taking a context: a killed process
	// is cleanup, and cleanup that can block forever is worse than cleanup
	// that is impolite.
	Close() error
}

// Launcher starts a server and hands back its stream.
//
// Separate from Transport because starting is where the failure a user
// actually has lives — the binary is not installed — and a test that wants to
// prove the missing-server path returns an error from here without needing a
// filesystem.
type Launcher interface {
	// Launch starts the server described by spec.
	//
	// The context is the server's LIFETIME, not a start timeout: cancelling it
	// terminates the process. An executable that cannot be found is an error,
	// which is the ordinary state of a machine without a language server
	// installed.
	Launch(ctx context.Context, spec language.ServerSpec) (Transport, error)
}
