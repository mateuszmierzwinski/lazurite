package lsp

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// The bounds a client applies when its caller supplied none.
//
// lazurite's own rule, learned from four measured defects: a call with no
// deadline of its own gets one here, because "the library will time out" is a
// claim to check and this one does not.
const (
	// defaultRequestTimeout is generous enough for a cold gopls answering its
	// first hover — which is slow, because it is still indexing — and short
	// enough that a wedged server does not hold a goroutine for the session.
	defaultRequestTimeout = 30 * time.Second
	// defaultHandshakeTimeout is how long a server has to answer initialize.
	// gopls on a large module takes seconds; a minute is not a server.
	defaultHandshakeTimeout = 60 * time.Second
	// defaultMaxRestarts is how many crashes are worth restarting through.
	// Past it the fault is the server, not the moment, and restarting forever
	// is a fork bomb with a timer on it.
	defaultMaxRestarts = 5
)

// client is one language server, seen from the editor.
//
// It OUTLIVES its sessions: a crash ends a session and the supervisor starts
// another, and the state that survives that — which documents are open, what
// the operator was told — lives here.
type client struct {
	spec     language.ServerSpec
	launcher Launcher
	sink     language.Sink
	reporter language.Reporter
	reopener language.Reopener
	lifetime context.Context

	requestTimeout   time.Duration
	handshakeTimeout time.Duration
	maxRestarts      int

	// mu protects sess, status, caps, docs and stopping. Every one of them is
	// read by callers on their own goroutines and written by the supervisor.
	mu       sync.RWMutex
	sess     *session
	status   language.Status
	caps     language.Capabilities
	docs     map[string]openDocument
	stopping bool

	// replaced is signalled whenever the supervisor adopts a new session, so a
	// test can wait for a restart instead of sleeping for one. Buffered to
	// one, and a send that would block is dropped: it is a nudge, not a queue.
	replaced chan struct{}

	starting sync.Once
}

// New builds a client for one server.
//
// Returns the INTERFACE (Rule 7.2). Nothing above this needs the concrete type,
// and a consumer that came to depend on it could not be handed `language.Absent`
// on a machine with no server installed — which is the whole degradation story.
func New(deps Deps) language.Client {
	return &client{
		spec: deps.Spec, launcher: launcherOr(deps.Launcher),
		sink: deps.Sink, reporter: deps.Reporter, reopener: deps.Reopener,
		lifetime:         lifetimeOr(deps.Lifetime),
		requestTimeout:   durationOr(deps.RequestTimeout, defaultRequestTimeout),
		handshakeTimeout: durationOr(deps.HandshakeTimeout, defaultHandshakeTimeout),
		maxRestarts:      restartsOr(deps.MaxRestarts),
		docs:             map[string]openDocument{},
		replaced:         make(chan struct{}, 1),
	}
}

// Status says whether language features are available right now.
func (c *client) Status() language.Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// Capabilities is what the server said it can do, and the zero value — which
// supports nothing — until it has said.
func (c *client) Capabilities() language.Capabilities {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.caps
}

// Request sends one request and decodes its result.
//
// The context bounds the wait. A caller that supplied no deadline gets this
// client's own, and a caller whose context ends first withdraws the request
// with `$/cancelRequest` — so a completion the operator has already typed past
// is DROPPED rather than applied late over what they are writing now.
func (c *client) Request(ctx context.Context, method string, params, out any) error {
	sess, err := c.live()
	if err != nil {
		return err
	}
	ctx, cancel := c.bounded(ctx)
	defer cancel()
	return c.call(ctx, sess, method, params, out)
}

// Notify sends one notification, which has no answer and therefore no wait.
func (c *client) Notify(ctx context.Context, method string, params any) error {
	sess, err := c.live()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return sess.notify(method, params)
}

// call is one request on a given session.
//
// The three ways out are the whole design: the server answers, the caller's
// context ends — in which case the request is withdrawn and forgotten, so a
// late answer has nowhere to land — or the session dies, which fails the call
// with a reason rather than leaving it to time out on something that was never
// going to reply.
func (c *client) call(ctx context.Context, sess *session, method string, params, out any) error {
	id, replies := sess.pending.register()
	request := wireRequest{JSONRPC: jsonRPCVersion, ID: id, Method: method, Params: params}
	if err := sess.send(request); err != nil {
		sess.pending.forget(id)
		return err
	}
	select {
	case reply := <-replies:
		if reply.err != nil {
			return reply.err
		}
		return decodeResult(reply.result, out)
	case <-ctx.Done():
		sess.pending.forget(id)
		_ = sess.notify(methodCancelRequest, cancelParams{ID: id})
		return ctx.Err()
	case <-sess.done:
		return ErrSessionGone
	}
}

// methodCancelRequest withdraws a request whose caller has gone.
const methodCancelRequest = "$/cancelRequest"

// live is the running session, or the reason there is not one.
//
// ErrNotRunning rather than a nil session, because every caller above this
// branches on it — and a client that is starting, crashed or gave up are three
// states a status bar tells apart while a call site does not need to.
func (c *client) live() (*session, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.sess == nil || c.status != language.StatusReady {
		return nil, language.ErrNotRunning
	}
	return c.sess, nil
}

// bounded gives a context a deadline where it has none.
func (c *client) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, has := ctx.Deadline(); has {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.requestTimeout)
}

// report tells the operator something they did not ask about.
//
// Falls back to stderr, because a degradation is worth saying even before there
// is a window to say it in — and a client with no reporter must not be a client
// that fails silently.
func (c *client) report(message string) {
	if c.reporter == nil {
		fmt.Fprintf(os.Stderr, "lazurite: %s\n", message)
		return
	}
	c.reporter.Report(message)
}

// name is the server's name for a message, falling back to its command.
func (c *client) name() string {
	if c.spec.Name != "" {
		return c.spec.Name
	}
	return c.spec.Command
}
