package lsp

import (
	"context"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// The document-synchronisation notifications, in the protocol's own words.
const (
	methodDidOpen   = "textDocument/didOpen"
	methodDidChange = "textDocument/didChange"
	methodDidSave   = "textDocument/didSave"
	methodDidClose  = "textDocument/didClose"
)

// DidOpen tells the server about a document and hands over its current text.
//
// Until this is called the server knows nothing about the file, whatever is on
// disk — an unsaved buffer is the case that matters, because it is exactly the
// one the operator wants diagnostics for.
func (c *client) DidOpen(ctx context.Context, doc language.Document) error {
	sess, err := c.live()
	if err != nil {
		return err
	}
	policy := c.docSync()
	if !policy.openClose {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sess.notify(methodDidOpen, openParamsOf(doc)); err != nil {
		return err
	}
	c.remember(doc)
	return nil
}

// DidChange reports edits to an already-open document.
//
// The version must ADVANCE. It is how a server discards an edit that overtook
// another one, and a client that let it repeat would leave the server deciding
// between two different texts claiming to be the same one.
//
// A server that negotiated whole-document sync requires a single whole-document
// change. This client keeps no copy of the operator's buffer to build one from
// — a second copy that drifts is worse than asking the caller for text it
// already has — so the mismatch is refused with a named error rather than
// papered over.
func (c *client) DidChange(ctx context.Context, path string, version int, changes []language.Change) error {
	sess, err := c.live()
	if err != nil {
		return err
	}
	policy := c.docSync()
	if policy.kind == language.SyncNone || len(changes) == 0 {
		return nil
	}
	if err := c.changeable(path, version, changes, policy); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := sess.notify(methodDidChange, changeParamsOf(path, version, changes)); err != nil {
		return err
	}
	c.advance(path, version)
	return nil
}

// changeable is every reason a change is refused before anything is sent.
//
// Separated from DidChange because the checks answer a different question from
// the send — what the server will accept, against what to tell it — and read
// together they are a body nobody finishes.
func (c *client) changeable(path string, version int, changes []language.Change, policy docSync) error {
	doc, open := c.opened(path)
	if !open {
		return language.ErrNotOpen
	}
	if version <= doc.version {
		return language.ErrStaleVersion
	}
	if policy.kind == language.SyncFull && !isWholeDocument(changes) {
		return language.ErrFullSyncRequired
	}
	return nil
}

// DidSave tells the server the document reached the disk.
//
// The text is sent only where the server asked to be sent it. A server that did
// not ask has the file on disk and can read it, and sending a megabyte it did
// not want on every Ctrl+S is the kind of cost that only shows up under a
// profiler.
func (c *client) DidSave(ctx context.Context, path, text string) error {
	sess, err := c.live()
	if err != nil {
		return err
	}
	policy := c.docSync()
	if !policy.save {
		return nil
	}
	if _, open := c.opened(path); !open {
		return language.ErrNotOpen
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return sess.notify(methodDidSave, saveParamsOf(path, text, policy.includeText))
}

// DidClose tells the server to forget a document.
//
// The document is forgotten HERE whether or not the notification got through:
// a client that kept it would refuse the next DidOpen for the same path as a
// duplicate, which is a file that can never be edited again this session.
func (c *client) DidClose(ctx context.Context, path string) error {
	sess, err := c.live()
	if err != nil {
		return err
	}
	policy := c.docSync()
	if !policy.openClose {
		return nil
	}
	if _, open := c.opened(path); !open {
		return language.ErrNotOpen
	}
	c.forget(path)
	if err := ctx.Err(); err != nil {
		return err
	}
	return sess.notify(methodDidClose, didCloseParams{
		TextDocument: textDocumentIdentifier{URI: pathToURI(path)},
	})
}

// docSync is what the negotiated capabilities mean for document notifications.
func (c *client) docSync() docSync {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return docSync{
		kind:        c.caps.Sync,
		openClose:   c.caps.OpenClose && c.caps.Sync != language.SyncNone,
		save:        c.caps.Save,
		includeText: c.caps.SaveIncludesText,
	}
}

// opened is what this client remembers about a path.
func (c *client) opened(path string) (openDocument, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	doc, open := c.docs[path]
	return doc, open
}

// remember records a document as open at its current version.
func (c *client) remember(doc language.Document) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.docs[doc.Path] = openDocument{
		path: doc.Path, languageID: languageIDOf(doc), version: doc.Version,
	}
}

// advance records that a document moved to a new version.
func (c *client) advance(path string, version int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	doc, open := c.docs[path]
	if !open {
		return
	}
	doc.version = version
	c.docs[path] = doc
}

// forget drops a closed document.
func (c *client) forget(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.docs, path)
}
