package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// clientVersion is what this editor tells a server it is. Some servers change
// behaviour on the client's identity, and one that will not say who it is
// invites being treated as the lowest common denominator.
const clientVersion = "0.1"

// The lifecycle methods, in the protocol's own words.
const (
	methodInitialize  = "initialize"
	methodInitialized = "initialized"
	methodShutdown    = "shutdown"
	methodExit        = "exit"
)

// Start brings the server up and completes the initialize handshake.
//
// It fails LOUDLY and degrades quietly: an error is returned so the composition
// root knows, the client is left in StatusFailed where every other call answers
// ErrNotRunning, and the operator is told once that they are working without
// language features. A missing binary — the ordinary state of a machine with no
// language server installed — arrives here as exactly that.
//
// Stop is terminal: a stopped client is not started again.
func (c *client) Start(ctx context.Context) error {
	if c.spec.Command == "" {
		c.giveUp(ErrNotAServerCommand)
		return ErrNotAServerCommand
	}
	if !c.claimStart() {
		return nil
	}
	sess, caps, err := c.dial(ctx)
	if err != nil {
		c.giveUp(err)
		return err
	}
	c.adopt(sess, caps)
	go c.supervise()
	return nil
}

// claimStart reserves the right to launch, refusing a second start while one
// is live and refusing any start after Stop.
func (c *client) claimStart() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess != nil || c.stopping {
		return false
	}
	c.status = language.StatusStarting
	return true
}

// dial launches a server, starts reading it, and shakes hands.
//
// The transport is given the client's LIFETIME rather than the handshake's
// context: cancelling ctx must abandon the handshake, not orphan a process
// that is already running.
func (c *client) dial(ctx context.Context) (*session, language.Capabilities, error) {
	transport, err := c.launcher.Launch(c.lifetime, c.spec)
	if err != nil {
		return nil, language.Capabilities{}, fmt.Errorf("%s: %w", c.name(), err)
	}
	sess := newSession(transport, c.sink, c.report)
	sess.start()
	caps, err := c.handshake(ctx, sess)
	if err != nil {
		sess.close()
		return nil, language.Capabilities{}, err
	}
	return sess, caps, nil
}

// handshake runs initialize and initialized.
//
// Bounded, always. A server that launches and never answers initialize is
// indistinguishable from a server that is not there, and a handshake with no
// deadline is how that becomes a window that never opens.
func (c *client) handshake(ctx context.Context, sess *session) (language.Capabilities, error) {
	ctx, cancel := context.WithTimeout(ctx, c.handshakeTimeout)
	defer cancel()
	var result initializeResult
	if err := c.call(ctx, sess, methodInitialize, c.opening(), &result); err != nil {
		return language.Capabilities{}, fmt.Errorf("%w: %s: %v", ErrHandshakeFailed, c.name(), err)
	}
	if err := sess.notify(methodInitialized, initializedParams{}); err != nil {
		return language.Capabilities{}, fmt.Errorf("%w: %s: %v", ErrHandshakeFailed, c.name(), err)
	}
	return capabilitiesOf(result.Capabilities), nil
}

// opening is the initialize request's parameters.
func (c *client) opening() initializeParams {
	root := pathToURI(c.spec.Root)
	return initializeParams{
		ProcessID:    os.Getpid(),
		ClientInfo:   clientInfo{Name: "lazurite", Version: clientVersion},
		RootURI:      root,
		Capabilities: editorCapabilities,
		Workspace:    []workspaceFolder{{URI: root, Name: filepath.Base(c.spec.Root)}},
	}
}

// adopt makes a freshly initialised session the live one.
func (c *client) adopt(sess *session, caps language.Capabilities) {
	c.mu.Lock()
	c.sess, c.caps, c.status = sess, caps, language.StatusReady
	// A new server knows about no documents. Clearing the record here is what
	// stops a DidChange being sent for a file this process was never told
	// about, which desynchronises it from the first keystroke.
	c.docs = map[string]openDocument{}
	c.mu.Unlock()
	select {
	case c.replaced <- struct{}{}:
	default:
	}
}

// giveUp puts the client in its terminal failed state and says so once.
func (c *client) giveUp(err error) {
	c.mu.Lock()
	c.sess, c.caps, c.status = nil, language.Capabilities{}, language.StatusFailed
	c.mu.Unlock()
	c.report(fmt.Sprintf(
		"%s is unavailable (%v); the editor is running with no language features",
		c.name(), err))
}

// supervise restarts a server that died, with a backoff, and gives up.
//
// GOROUTINE OWNERSHIP (Rule 20.6). One per client, started by Start. It waits
// for the live session's reader goroutine to end, then decides: restart after a
// backoff, or stop trying. It TERMINATES when Stop is called, when the client's
// lifetime context is done, or when the restart budget runs out — at which
// point the client is StatusFailed and the editor has no language features,
// which is the promised degradation rather than a broken editor.
//
// A successful restart resets the budget, so a server that runs for a week and
// then crashes gets its five attempts again rather than inheriting the count
// from the last time.
func (c *client) supervise() {
	defer survive("language server supervisor", c.report)
	for attempt := 1; c.awaitExit(); attempt++ {
		if attempt > c.maxRestarts {
			c.giveUp(ErrSessionGone)
			return
		}
		if !c.pause(backoff(attempt)) {
			return
		}
		if c.redial() {
			attempt = 0
		}
	}
}

// awaitExit blocks until the live session ends, and says whether restarting is
// still wanted.
func (c *client) awaitExit() bool {
	sess := c.current()
	if sess == nil {
		return !c.finished()
	}
	select {
	case <-sess.done:
	case <-c.lifetime.Done():
	}
	sess.close()
	if c.finished() {
		return false
	}
	c.detach()
	return true
}

// redial starts a replacement server and asks for the open documents back.
func (c *client) redial() bool {
	sess, caps, err := c.dial(c.lifetime)
	if err != nil {
		return false
	}
	c.adopt(sess, caps)
	c.reopen()
	return true
}

// reopen asks the consumer to re-announce every open document.
//
// A restarted server is a new process with an empty view of the workspace, so
// without this there are no diagnostics for anything the operator has open —
// which reads as the language server having quietly stopped working. The client
// cannot do it alone: it deliberately keeps no copy of the buffers.
func (c *client) reopen() {
	if c.reopener == nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.lifetime, c.requestTimeout)
	defer cancel()
	if err := c.reopener.Reopen(ctx, c); err != nil {
		c.report("could not re-open documents after restarting " + c.name() + ": " + err.Error())
	}
}

// pause waits out a backoff, and says whether to carry on afterwards.
func (c *client) pause(wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return !c.finished()
	case <-c.lifetime.Done():
		return false
	}
}

// Stop performs the shutdown/exit handshake and ends the process.
//
// Idempotent, and terminal. The context bounds how long a polite exit is waited
// for; past it the transport is closed and the process is killed, because a
// server refusing to leave must not outlive the window.
func (c *client) Stop(ctx context.Context) error {
	sess := c.beginStop()
	if sess == nil {
		return nil
	}
	c.farewell(ctx, sess)
	sess.close()
	return nil
}

// beginStop marks the client stopped and takes the live session away from it,
// so nothing else can send on a server that is leaving.
func (c *client) beginStop() *session {
	c.mu.Lock()
	defer c.mu.Unlock()
	sess := c.sess
	c.sess, c.stopping, c.status = nil, true, language.StatusStopped
	c.docs = map[string]openDocument{}
	return sess
}

// farewell asks the server to shut down, then to exit.
//
// A shutdown that fails skips the exit notification deliberately: the protocol
// says exit follows shutdown, and a server that did not answer the first is not
// going to act on the second. Closing the transport is what actually ends it.
func (c *client) farewell(ctx context.Context, sess *session) {
	ctx, cancel := c.bounded(ctx)
	defer cancel()
	if err := c.call(ctx, sess, methodShutdown, nil, nil); err != nil {
		return
	}
	_ = sess.notify(methodExit, nil)
}

// current is the live session, or nil.
func (c *client) current() *session {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sess
}

// finished says whether the client is stopping or its lifetime has ended.
func (c *client) finished() bool {
	c.mu.RLock()
	stopping := c.stopping
	c.mu.RUnlock()
	return stopping || c.lifetime.Err() != nil
}

// detach forgets a dead session, so a call made during the backoff is refused
// rather than written to a closed pipe.
func (c *client) detach() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sess, c.caps, c.status = nil, language.Capabilities{}, language.StatusStarting
	c.docs = map[string]openDocument{}
}

// editorCodeActions is what this editor can do with an action a server offers.
//
// Literal support, so an action arrives carrying its own edit rather than a
// command whose work would come back as a `workspace/applyEdit` this client
// refuses. The kinds are the families the window has controls for; they are
// hierarchical, so "refactor" covers every leaf under it.
//
// RESOLVE is declared, and it is what makes refactoring work at all: gopls
// answers "Extract function" with a title and a token and computes the change
// only when asked, because working it out costs a type-check of the package. A
// client that does not declare this is offered the same actions as COMMANDS it
// cannot run, and silently shows nothing (querydecode.runnableAction).
var editorCodeActions = codeActionCapability{
	CodeActionLiteral: codeActionLiteral{CodeActionKind: codeActionKinds{
		ValueSet: []string{"quickfix", "refactor", "source", kindOrganiseImports},
	}},
	IsPreferredSupport: true,
	DisabledSupport:    true,
	DataSupport:        true,
	ResolveSupport:     &codeActionResolve{Properties: []string{"edit"}},
}

// editorCapabilities is what this editor promises a server, built once at
// package scope. See clientCapabilities for what is deliberately absent.
var editorCapabilities = clientCapabilities{
	Workspace: workspaceCapabilities{
		WorkspaceFolders: true, ApplyEdit: false,
		WorkspaceEdit: workspaceEditCapability{DocumentChanges: true},
	},
	TextDocument: textDocumentCapabilities{
		Synchronization: syncCapability{DidSave: true},
		Hover:           contentCapability{ContentFormat: []string{"markdown", "plaintext"}},
		SignatureHelp:   contentCapability{ContentFormat: []string{"markdown", "plaintext"}},
		Completion: completionCapability{
			CompletionItem: completionItem{
				DocumentationFormat: []string{"markdown", "plaintext"},
				DeprecatedSupport:   true,
				LabelDetailsSupport: true,
			},
		},
		// Link support on all three "where is it" questions, because
		// `linkLocations` reads that form and prefers the selection range in
		// it — which is what makes a jump land on a symbol's name rather than
		// on the first line of its declaration.
		Definition:     linkCapability{LinkSupport: true},
		TypeDefinition: linkCapability{LinkSupport: true},
		Implementation: linkCapability{LinkSupport: true},
		CodeAction:     editorCodeActions,
		Rename:         renameCapability{PrepareSupport: true},
	},
}
