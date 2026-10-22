// Package gopls is what knows Go: completions, definitions, descriptions
// and what is wrong, asked of the language server `gopls` through the
// shared LSP client.
//
// Every question is put on one queue and asked by one goroutine, in order.
// That is what keeps the server's picture of a file in step with the
// editor's — a completion asked after a keystroke is asked after the
// keystroke has been sent — and what lets every call here return at once.
package gopls

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/lsp"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
)

// Server is a running language server and the files it has been told of.
//
// mu protects texts, versions and heard: they are written by the worker
// and read by the goroutine the server's notifications arrive on.
type Server struct {
	client language.Client
	ctx    context.Context
	stop   context.CancelFunc
	// work is the queue of things to do, in order. 256 deep: a burst of
	// keystrokes is queued without the editor waiting; beyond that the
	// editor waits, which is the back-pressure wanted.
	work chan func()
	wg   sync.WaitGroup

	mu       sync.Mutex
	texts    map[string]string
	versions map[string]int
	heard    func(path string, problems []ide.Problem)
}

// The patience shown to the server.
const (
	// askTimeout is how long one question may take.
	askTimeout = 8 * time.Second
	// startTimeout is how long the server may take to start.
	startTimeout = 30 * time.Second
)

// New starts the language server for a project and answers as the IDE's
// port. The server is started in the background: the editor is usable at
// once, and questions asked before the server is ready wait their turn. A
// server that cannot be started answers nothing, and the editor works
// without it.
//
// It starts one goroutine, the worker, which ends when Stop is called or
// the context ends.
func New(ctx context.Context, command, root string) ide.Intel {
	return start(ctx, func(s *Server) language.Client {
		return lsp.New(lsp.Deps{
			Spec:     language.ServerSpec{Name: "gopls", Command: command, Extensions: []string{".go"}, Root: root},
			Launcher: lsp.NewLauncher(),
			Sink:     s, Reporter: s, Reopener: s,
			Lifetime: s.ctx, RequestTimeout: askTimeout, HandshakeTimeout: startTimeout,
		})
	})
}

// start builds a server around a client and sets its worker going. The
// client is made by a function given the server, because the two know each
// other: the client reports to the server it is asked through.
func start(ctx context.Context, client func(*Server) language.Client) *Server {
	ctx, stop := context.WithCancel(ctx)
	s := &Server{
		ctx: ctx, stop: stop,
		// 256: see the field.
		work:     make(chan func(), 256),
		texts:    map[string]string{},
		versions: map[string]int{},
	}
	s.client = client(s)
	s.wg.Add(1)
	go s.run()
	s.do(func() { _ = s.client.Start(s.ctx) })
	return s
}

// run is the worker: it does what is queued, in order, until stopped.
func (s *Server) run() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case job := <-s.work:
			job()
		}
	}
}

// do queues something for the worker. After Stop it is dropped.
func (s *Server) do(job func()) {
	select {
	case s.work <- job:
	case <-s.ctx.Done():
	}
}

// Listen names who is told what is wrong with a file.
func (s *Server) Listen(heard func(path string, problems []ide.Problem)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.heard = heard
}

// remember keeps a file's text and gives it its next version.
func (s *Server) remember(path, text string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts[path] = text
	s.versions[path]++
	return s.versions[path]
}

// text is a file's text as the server was last told it.
func (s *Server) text(path string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.texts[path]
}

// Open tells the server a file is being edited.
func (s *Server) Open(path, text string) {
	s.do(func() {
		version := s.remember(path, text)
		_ = s.client.DidOpen(s.ctx, language.Document{Path: path, LanguageID: language.LanguageID(path), Version: version, Text: text})
	})
}

// Change tells the server a file's new text.
func (s *Server) Change(path, text string) {
	s.do(func() {
		if s.text(path) == text {
			return
		}
		version := s.remember(path, text)
		_ = s.client.DidChange(s.ctx, path, version, language.FullChange(text))
	})
}

// Save tells the server a file was written.
func (s *Server) Save(path, text string) {
	s.do(func() { _ = s.client.DidSave(s.ctx, path, text) })
}

// Close tells the server a file is no longer being edited.
func (s *Server) Close(path string) {
	s.do(func() {
		s.mu.Lock()
		delete(s.texts, path)
		delete(s.versions, path)
		s.mu.Unlock()
		_ = s.client.DidClose(s.ctx, path)
	})
}

// Stop ends the server and the worker, and waits for the worker.
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 2*time.Second)
	defer cancel()
	_ = s.client.Stop(ctx)
	s.stop()
	s.wg.Wait()
}

// Complete asks what could be typed at a place.
func (s *Server) Complete(path string, at edit.Pos, done func([]ide.Suggestion)) {
	s.do(func() {
		text := s.text(path)
		answer := lsp.Ask(s.ctx, s.client, language.Query{
			Feature: language.FeatureCompletion, Path: path, Position: position(text, at),
		})
		if answer.Err != nil {
			done(nil)
			return
		}
		done(suggestions(text, answer.Completions.Items))
	})
}

// Define asks where the name at a place is defined.
func (s *Server) Define(path string, at edit.Pos, done func(ide.Place, bool)) {
	s.do(func() {
		answer := lsp.Ask(s.ctx, s.client, language.Query{
			Feature: language.FeatureDefinition, Path: path, Position: position(s.text(path), at),
		})
		if answer.Err != nil || len(answer.Locations) == 0 {
			done(ide.Place{}, false)
			return
		}
		where := answer.Locations[0]
		done(ide.Place{Path: where.Path, At: caret(s.text(where.Path), where.Range.Start)}, true)
	})
}

// Describe asks what is known about the name at a place.
func (s *Server) Describe(path string, at edit.Pos, done func(string)) {
	s.do(func() {
		answer := lsp.Ask(s.ctx, s.client, language.Query{
			Feature: language.FeatureHover, Path: path, Position: position(s.text(path), at),
		})
		done(answer.Hover.Text)
	})
}

// Publish takes a notification from the server; one that says what is
// wrong with a file is passed on.
func (s *Server) Publish(n language.Notification) {
	pub, ok := lsp.Published(n)
	if !ok {
		return
	}
	s.mu.Lock()
	heard, text := s.heard, s.texts[pub.Path]
	s.mu.Unlock()
	if heard != nil {
		heard(pub.Path, problems(text, pub.Diagnostics))
	}
}

// Report takes the server's remarks about itself. They are not shown: a
// server that is absent is one the editor simply works without.
func (s *Server) Report(string) {
	// Deliberately unheard.
}

// Reopen tells a restarted server about the files being edited again.
func (s *Server) Reopen(ctx context.Context, client language.Client) error {
	s.mu.Lock()
	open := make(map[string]string, len(s.texts))
	for path, text := range s.texts {
		open[path] = text
	}
	s.mu.Unlock()
	paths := make([]string, 0, len(open))
	for path := range open {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		doc := language.Document{Path: path, LanguageID: language.LanguageID(path), Version: s.remember(path, open[path]), Text: open[path]}
		if err := client.DidOpen(ctx, doc); err != nil {
			return err
		}
	}
	return nil
}
