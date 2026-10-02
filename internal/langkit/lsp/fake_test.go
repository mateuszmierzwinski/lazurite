package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/framing"
	"io"
	"sync"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// The test harness: an in-process server speaking the same protocol over the
// same framing, on a pair of pipes.
//
// No test in this package starts a real language server, touches the network or
// touches the operator's desktop. `gopls` is not installed on the machine this
// was written on, and a test that needed it would be a test that fails wherever
// it is not — which is most machines.

// pipeTransport is the client's end of an in-memory stdio pair.
type pipeTransport struct {
	reader  *io.PipeReader
	writer  *io.PipeWriter
	closing sync.Once
}

// Read is the server's stdout.
func (p *pipeTransport) Read(b []byte) (int, error) { return p.reader.Read(b) }

// Write is the server's stdin.
func (p *pipeTransport) Write(b []byte) (int, error) { return p.writer.Write(b) }

// Close unblocks the reader goroutine, which is what a real transport's Close
// must also do.
func (p *pipeTransport) Close() error {
	p.closing.Do(func() {
		_ = p.reader.Close()
		_ = p.writer.Close()
	})
	return nil
}

// fakeServer answers the client the way a language server would.
type fakeServer struct {
	in   *io.PipeReader
	out  *io.PipeWriter
	caps string

	// silent answers nothing at all, which is how a wedged server is tested.
	silent bool
	// results are canned answers, keyed by method.
	results map[string]string
	// ignore is the methods this server accepts and never answers, which is
	// how a wedged server is tested without wedging the test.
	ignore map[string]bool

	mu       sync.Mutex // protects seen
	seen     []incoming
	arrived  chan struct{}
	stopping sync.Once
}

// defaultCaps is a capability set close to what gopls answers with.
const defaultCaps = `{
  "textDocumentSync":{"openClose":true,"change":2,"save":{"includeText":false}},
  "hoverProvider":true,
  "definitionProvider":true,
  "referencesProvider":{"workDoneProgress":true},
  "renameProvider":{"prepareProvider":true},
  "completionProvider":{"triggerCharacters":[".","\""]},
  "signatureHelpProvider":{"triggerCharacters":["(",","]},
  "documentFormattingProvider":true,
  "codeActionProvider":true,
  "inlayHintProvider":true,
  "semanticTokensProvider":{"legend":{"tokenTypes":[],"tokenModifiers":[]}}
}`

// newFakePair wires a client transport to a fake server and starts it.
func newFakePair(caps string) (*pipeTransport, *fakeServer) {
	upReader, upWriter := io.Pipe()
	downReader, downWriter := io.Pipe()
	client := &pipeTransport{reader: downReader, writer: upWriter}
	server := &fakeServer{
		in: upReader, out: downWriter, caps: caps,
		results: map[string]string{}, ignore: map[string]bool{},
		arrived: make(chan struct{}, 64),
	}
	go server.serve()
	return client, server
}

// serve reads what the client sent and answers it.
func (s *fakeServer) serve() {
	reader := bufio.NewReader(s.in)
	for {
		body, err := framing.ReadFrame(reader)
		if err != nil {
			return
		}
		var msg incoming
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		s.record(msg)
		if !s.silent {
			s.answer(msg)
		}
	}
}

// record keeps what arrived so a test can assert on it.
func (s *fakeServer) record(msg incoming) {
	s.mu.Lock()
	s.seen = append(s.seen, msg)
	s.mu.Unlock()
	select {
	case s.arrived <- struct{}{}:
	default:
	}
}

// answer replies to a request, and says nothing to a notification.
func (s *fakeServer) answer(msg incoming) {
	if len(msg.ID) == 0 || s.ignore[msg.Method] {
		return
	}
	switch {
	case msg.Method == methodInitialize:
		s.respond(msg.ID, `{"capabilities":`+s.caps+`}`)
	case msg.Method == methodShutdown:
		s.respond(msg.ID, `null`)
	default:
		s.canned(msg)
	}
}

// canned answers from the table, or says the method is not implemented.
func (s *fakeServer) canned(msg incoming) {
	if result, ok := s.results[msg.Method]; ok {
		s.respond(msg.ID, result)
		return
	}
	s.reject(msg.ID, codeMethodNotFound, "the fake server does not implement "+msg.Method)
}

// respond writes a successful answer.
func (s *fakeServer) respond(id json.RawMessage, result string) {
	s.write(`{"jsonrpc":"2.0","id":` + string(id) + `,"result":` + result + `}`)
}

// reject writes an error answer.
func (s *fakeServer) reject(id json.RawMessage, code int, message string) {
	body, _ := json.Marshal(wireError{Code: code, Message: message})
	s.write(`{"jsonrpc":"2.0","id":` + string(id) + `,"error":` + string(body) + `}`)
}

// notify pushes an unsolicited notification, which is how diagnostics arrive.
func (s *fakeServer) notify(method, params string) {
	s.write(`{"jsonrpc":"2.0","method":"` + method + `","params":` + params + `}`)
}

// ask makes a request OF the client, which the client must answer.
func (s *fakeServer) ask(id int, method, params string) {
	s.write(`{"jsonrpc":"2.0","id":` + itoaTest(id) + `,"method":"` + method + `","params":` + params + `}`)
}

// write frames one message onto the wire.
func (s *fakeServer) write(body string) {
	_ = framing.WriteFrame(s.out, []byte(body))
}

// raw writes bytes that are not a valid frame, which is how the framing bounds
// are exercised against a live client rather than against readFrame alone.
func (s *fakeServer) raw(bytes string) {
	_, _ = s.out.Write([]byte(bytes))
}

// crash ends the server without a word, the way a segfaulting one does.
func (s *fakeServer) crash() {
	s.stopping.Do(func() {
		_ = s.out.Close()
		_ = s.in.Close()
	})
}

// sent is every message the client sent, copied.
func (s *fakeServer) sent() []incoming {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]incoming{}, s.seen...)
}

// await waits for the client to have sent a message with this method.
func (s *fakeServer) await(method string, within time.Duration) bool {
	deadline := time.After(within)
	for {
		if s.saw(method) {
			return true
		}
		select {
		case <-s.arrived:
		case <-deadline:
			return s.saw(method)
		}
	}
}

// saw says whether a method has arrived yet.
func (s *fakeServer) saw(method string) bool {
	for _, msg := range s.sent() {
		if msg.Method == method {
			return true
		}
	}
	return false
}

// awaitReply waits for the client to answer a request the server made.
func (s *fakeServer) awaitReply(id string, within time.Duration) (incoming, bool) {
	deadline := time.After(within)
	for {
		for _, msg := range s.sent() {
			if msg.Method == "" && string(msg.ID) == id {
				return msg, true
			}
		}
		select {
		case <-s.arrived:
		case <-deadline:
			return incoming{}, false
		}
	}
}

// waitFor polls a condition, so a test observes a background goroutine's work
// instead of sleeping for a guess at how long it takes.
func waitFor(within time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return condition()
}

// itoaTest keeps the fake free of a strconv import in every helper.
func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// fakeLauncher hands out fake servers instead of processes.
type fakeLauncher struct {
	mu       sync.Mutex
	caps     string
	failure  error
	silent   bool
	servers  []*fakeServer
	launches int
}

// Launch builds a fresh fake server, or fails the way a missing binary does.
func (l *fakeLauncher) Launch(context.Context, language.ServerSpec) (Transport, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.launches++
	if l.failure != nil {
		return nil, l.failure
	}
	transport, server := newFakePair(l.caps)
	server.silent = l.silent
	l.servers = append(l.servers, server)
	return transport, nil
}

// latest is the most recently launched server.
func (l *fakeLauncher) latest() *fakeServer {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.servers) == 0 {
		return nil
	}
	return l.servers[len(l.servers)-1]
}

// started is how many times a server was launched, which is how a restart is
// observed without sleeping for one.
func (l *fakeLauncher) started() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.launches
}

// recordingSink keeps the notifications the client published.
type recordingSink struct {
	mu   sync.Mutex
	seen []language.Notification
	got  chan struct{}
}

// newRecordingSink builds a sink a test can wait on.
func newRecordingSink() *recordingSink {
	return &recordingSink{got: make(chan struct{}, 64)}
}

// Publish records one notification.
func (r *recordingSink) Publish(n language.Notification) {
	r.mu.Lock()
	r.seen = append(r.seen, n)
	r.mu.Unlock()
	select {
	case r.got <- struct{}{}:
	default:
	}
}

// notifications is what arrived, copied.
func (r *recordingSink) notifications() []language.Notification {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]language.Notification{}, r.seen...)
}

// recordingReporter keeps what the operator would have been told.
type recordingReporter struct {
	mu       sync.Mutex
	messages []string
}

// Report records one message.
func (r *recordingReporter) Report(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, message)
}

// said is what was reported, copied.
func (r *recordingReporter) said() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.messages...)
}
