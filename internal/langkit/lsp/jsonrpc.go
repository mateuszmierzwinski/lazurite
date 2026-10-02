package lsp

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
)

// answer is one request's outcome, either the server's result or the reason
// there will not be one.
type answer struct {
	result json.RawMessage
	err    error
}

// pending is the requests this client is waiting on.
//
// The map is read by the READER goroutine, which delivers answers, and written
// by every caller that makes a request — so it is behind a mutex (Rule 20.1),
// and `mu` protects `calls` and `next` together.
type pending struct {
	mu    sync.Mutex // protects calls and next
	next  int64
	calls map[int64]chan answer
}

// newPending builds an empty request map.
func newPending() *pending { return &pending{calls: map[int64]chan answer{}} }

// register allocates an id and the channel its answer will arrive on.
//
// The channel is BUFFERED to one. The reader goroutine must never block
// delivering an answer: a caller whose context expired has already stopped
// listening, and an unbuffered send would wedge the reader — and with it every
// other request and every diagnostic — on one abandoned call.
func (p *pending) register() (int64, chan answer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	replies := make(chan answer, 1)
	p.calls[p.next] = replies
	return p.next, replies
}

// deliver hands an answer to whoever is waiting for it.
//
// An id nobody is waiting for is DROPPED, which is the normal outcome of a
// cancelled request: the caller went away, `forget` removed the entry, and the
// server answered anyway. That answer is stale and applying it late is the
// defect this whole mechanism exists to prevent.
func (p *pending) deliver(id int64, a answer) {
	p.mu.Lock()
	replies, waiting := p.calls[id]
	delete(p.calls, id)
	p.mu.Unlock()
	if !waiting {
		return
	}
	replies <- a
}

// forget drops a request whose caller has stopped waiting.
func (p *pending) forget(id int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.calls, id)
}

// failAll ends every outstanding request with the same reason.
//
// Called once, by the reader goroutine on its way out. Without it a server that
// dies mid-request leaves its callers blocked until their own contexts expire —
// which they eventually do, but reporting "the server went away" beats
// reporting a timeout for something that was never going to answer.
func (p *pending) failAll(err error) {
	p.mu.Lock()
	calls := p.calls
	p.calls = map[int64]chan answer{}
	p.mu.Unlock()
	for _, replies := range calls {
		replies <- answer{err: err}
	}
}

// messageKind is which of the three JSON-RPC shapes arrived.
type messageKind int

// The three shapes, told apart by which fields are present.
const (
	// kindResponse is an id with no method: an answer to something we asked.
	kindResponse messageKind = iota
	// kindRequest is an id WITH a method: the server asking us something, and
	// it must be answered or the server waits.
	kindRequest
	// kindNotification is a method with no id: something the server is
	// telling us, with no answer expected.
	kindNotification
)

// classify says which shape a message is.
func classify(msg incoming) messageKind {
	switch {
	case msg.Method == "":
		return kindResponse
	case len(msg.ID) == 0:
		return kindNotification
	default:
		return kindRequest
	}
}

// requestID reads back an id this client allocated.
//
// False for anything that is not one of our integers — a server answering with
// a string id is answering something we never sent, and matching it to a
// numeric request by coincidence would hand one caller another's result.
func requestID(raw json.RawMessage) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
