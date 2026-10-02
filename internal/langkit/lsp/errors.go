package lsp

import (
	"errors"
	"strconv"
)

// The framing and lifecycle errors.
//
// Every one of them is FATAL to the stream it happened on. A JSON-RPC message
// stream framed by Content-Length cannot be resynchronised after a bad header:
// the only thing that says where the next message starts is the length of this
// one, so a client that "skipped a bad frame" would be guessing, and every
// message after it would be read out of somebody else's bytes. The session dies
// and the supervisor decides whether to restart it.
var (

	// ErrNotAServerCommand is a spec with no command in it. Refused at launch
	// rather than handed to exec, so the failure names the configuration.
	ErrNotAServerCommand = errors.New("lsp: server spec names no command")

	// ErrHandshakeFailed is a server that started and did not initialise.
	ErrHandshakeFailed = errors.New("lsp: server did not complete the initialize handshake")

	// ErrSessionGone is a request outstanding when the server went away. Every
	// caller waiting on an answer is failed with this rather than left blocked.
	ErrSessionGone = errors.New("lsp: server went away before answering")

	// ErrBadURI is a document URI that is not a local file. A server naming
	// `http://` or a remote host is answering with something the editor must
	// not open.
	ErrBadURI = errors.New("lsp: not a local file URI")
)

// remoteError is an error the SERVER returned, carrying its own code.
//
// A named type rather than a formatted string because the caller branches on
// the code: -32601 (method not found) means the server does not implement this
// and the capability check missed it, where -32603 means it tried and failed.
type remoteError struct {
	Code    int
	Message string
}

// Error renders the server's own words, tagged so a log says where they came
// from.
func (e *remoteError) Error() string {
	return "lsp: server error " + strconv.Itoa(e.Code) + ": " + e.Message
}
