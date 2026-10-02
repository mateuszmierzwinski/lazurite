package lsp

import (
	"bufio"
	"encoding/json"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/framing"
	"sync"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// session is ONE running server process and the conversation with it.
//
// A client outlives its sessions: a crash ends this one and the supervisor
// starts another. Everything that dies with the process — the transport, the
// reader goroutine, the outstanding requests — lives here rather than on the
// client, so a restart cannot inherit half of a dead server's state.
type session struct {
	transport Transport
	reader    *bufio.Reader
	pending   *pending
	sink      language.Sink
	report    func(string)

	// writeMu serialises writes to the server's stdin. Two frames interleaved
	// on one pipe are two corrupt messages, and every caller writes from its
	// own goroutine.
	writeMu sync.Mutex

	// answers carries the server's own requests to the responder goroutine.
	// The reader must NEVER write: a server that floods its stdout while not
	// reading its stdin would otherwise deadlock the client outright, with the
	// reader blocked writing an answer and the server blocked writing the next
	// message nobody is reading.
	answers chan incoming

	// done is closed when the reader goroutine has ended, and is what the
	// supervisor waits on. failure is why, and is written before the close so
	// the channel gives the happens-before that makes it safe to read after.
	done    chan struct{}
	failure error

	wg      sync.WaitGroup
	closing sync.Once
}

// newSession wraps a live transport. The reader goroutine is not running yet;
// start does that.
func newSession(transport Transport, sink language.Sink, report func(string)) *session {
	return &session{
		transport: transport,
		reader:    bufio.NewReaderSize(transport, readBufferBytes),
		pending:   newPending(),
		sink:      sink,
		report:    report,
		answers:   make(chan incoming, answersDepth),
		done:      make(chan struct{}),
	}
}

// readBufferBytes is the reader's buffer. Large enough that an ordinary
// response is one syscall, small enough that ten idle servers cost nothing.
const readBufferBytes = 64 << 10

// answersDepth is how many unanswered server requests may queue up.
//
// Deep enough that a burst — a server registering six capabilities at once —
// never touches it, and shallow enough that a server asking faster than it
// reads is refused rather than accumulated. A full queue means the server has
// stopped reading its own stdin, at which point the session is finished
// whatever this client does.
const answersDepth = 32

// start runs the reader goroutine.
//
// GOROUTINE OWNERSHIP (Rule 20.6). The goroutine started here reads framed
// messages from the server's stdout and dispatches each one: an answer to a
// pending request, a notification to the sink, or a request the server made of
// us. It is the ONLY reader of the transport.
//
// It TERMINATES when a Read returns an error — which happens when the server
// exits, when its stdout is closed, or when `close` closes the transport under
// it — or when a frame is malformed, which cannot be recovered from because the
// only thing saying where the next message begins is the length of this one.
//
// Its completion is synchronised two ways, and both are used: `wg` for a caller
// that must not return until it has stopped, and `done` for the supervisor,
// which waits on it to decide whether to restart. It touches no widget and
// knows nothing about one; what it has to say leaves through the Sink.
func (s *session) start() {
	s.wg.Add(2)
	go s.read()
	go s.respond()
}

// read is the reader goroutine's body. See start for its lifecycle.
func (s *session) read() {
	var failure error
	// Declared first so it runs LAST: whatever else happens, the pending
	// requests are failed and `done` is closed exactly once.
	defer s.stopped(&failure)
	// Declared second so it runs FIRST on a panic. A defect in decoding a
	// hostile server's message must cost the operator this server, not the
	// window they were working in.
	defer survive("language server reader", s.report)
	for {
		body, err := framing.ReadFrame(s.reader)
		if err != nil {
			failure = err
			return
		}
		s.dispatch(body)
	}
}

// stopped ends the session: every caller waiting on an answer is told, the
// responder is released, and the supervisor is woken.
//
// The reader is the only sender on `answers`, which is what makes closing it
// here safe rather than a race waiting to happen.
func (s *session) stopped(failure *error) {
	s.failure = *failure
	if s.failure == nil {
		s.failure = ErrSessionGone
	}
	close(s.answers)
	s.pending.failAll(ErrSessionGone)
	close(s.done)
	s.wg.Done()
}

// respond is the responder goroutine's body.
//
// GOROUTINE OWNERSHIP (Rule 20.6). One per session, started beside the reader.
// It answers the requests the SERVER makes of this client, and exists so that
// the reader never blocks on a write — see `answers`. It TERMINATES when the
// reader closes that channel, which the reader does exactly once on its way
// out, and its completion is part of the same WaitGroup `close` waits on.
func (s *session) respond() {
	defer s.wg.Done()
	for msg := range s.answers {
		s.answerSafely(msg)
	}
}

// answerSafely answers one request, surviving a panic in the handling of it.
//
// Per MESSAGE rather than per goroutine. A defect answering one request must
// not leave the responder dead and every later server request unanswered —
// which is a session that looks alive and has quietly stopped holding up its
// half of the protocol, the worst of the three outcomes.
func (s *session) answerSafely(msg incoming) {
	defer survive("language server responder", s.report)
	s.answer(msg)
}

// dispatch routes one decoded message.
//
// A body that is not JSON is REPORTED and skipped rather than fatal: the frame
// itself was well-formed, so the stream is still in step and the next message
// starts where it should. That is the one recoverable class of malformed input
// here, and telling it apart from a framing fault is what keeps a single bad
// notification from taking a working server down.
func (s *session) dispatch(body []byte) {
	var msg incoming
	if err := json.Unmarshal(body, &msg); err != nil {
		s.report("language server sent a message that is not JSON-RPC: " + err.Error())
		return
	}
	switch classify(msg) {
	case kindResponse:
		s.deliver(msg)
	case kindNotification:
		s.publish(msg)
	case kindRequest:
		s.serve(msg)
	}
}

// deliver hands a response to the caller waiting on it.
func (s *session) deliver(msg incoming) {
	id, ours := requestID(msg.ID)
	if !ours {
		return
	}
	if msg.Error != nil {
		s.pending.deliver(id, answer{err: &remoteError{Code: msg.Error.Code, Message: msg.Error.Message}})
		return
	}
	s.pending.deliver(id, answer{result: msg.Result})
}

// publish hands a notification to the sink.
//
// The sink is the CONSUMER's code — T5.2's diagnostics, T5.3's progress —
// running on our goroutine, so it gets its own recovery: a defect in decoding
// diagnostics must not take the language server down with it.
func (s *session) publish(msg incoming) {
	defer survive("language notification sink", s.report)
	if s.sink == nil {
		return
	}
	s.sink.Publish(language.Notification{Method: msg.Method, Params: msg.Params})
}

// serve hands a request the server made to the responder goroutine.
//
// Every server request is answered, including one this client does not
// implement: a server left waiting on an answer stalls whatever it was doing
// on the editor's behalf, and "method not found" is a better answer than
// silence. It is answered on ANOTHER goroutine because writing it here would
// block the reader, which is the one thing the reader must never do.
func (s *session) serve(msg incoming) {
	select {
	case s.answers <- msg:
	default:
		s.report("language server: too many unanswered requests; dropped " + msg.Method)
	}
}

// answer writes the reply to one server request, on the responder goroutine.
func (s *session) answer(msg incoming) {
	result, failure := handleServerRequest(msg.Method, msg.Params)
	response := wireResponse{JSONRPC: jsonRPCVersion, ID: msg.ID, Result: result, Error: failure}
	if err := s.send(response); err != nil {
		s.report("language server: could not answer " + msg.Method + ": " + err.Error())
	}
}

// send writes one message, serialised against every other writer.
//
// A write to a server that has stopped reading blocks until `close` closes the
// pipe under it, which is the bound: there is no deadline on a pipe write, and
// a writer goroutine with a queue in front of it would buy nothing except a
// place for messages to pile up unsent.
func (s *session) send(msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return framing.WriteFrame(s.transport, body)
}

// notify sends a notification, which has no answer.
func (s *session) notify(method string, params any) error {
	return s.send(wireNotification{JSONRPC: jsonRPCVersion, Method: method, Params: params})
}

// close ends the transport and waits for the reader goroutine.
//
// Idempotent, because it is called both by a deliberate Stop and by the
// supervisor tidying up after a crash. Closing the transport is what unblocks
// the reader's Read; the wait is what makes "the session is over" true rather
// than merely likely.
func (s *session) close() {
	s.closing.Do(func() { _ = s.transport.Close() })
	s.wg.Wait()
}
