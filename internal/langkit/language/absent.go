package language

import "context"

// Absent is the client for a file no language server handles.
//
// A NULL OBJECT, and the reason nothing above this package needs a nil check.
// The requirement T5.1 was given is that a server which is missing, crashed or
// silent degrades to no language features and never to a broken editor; the
// cheapest way to keep that promise is for "no server" to be a Client like any
// other, answering ErrNoServer to everything and supporting nothing.
//
// Returns the INTERFACE (Rule 7.2), so a caller cannot come to depend on the
// fact that this one happens to be free.
func Absent() Client { return absent{} }

// absent refuses every operation without failing.
type absent struct{}

// Start succeeds, because there is nothing to start and a caller that had to
// tolerate an error here would tolerate a real one too.
func (absent) Start(context.Context) error { return nil }

// Status is stopped: not failed, because nothing went wrong.
func (absent) Status() Status { return StatusStopped }

// Capabilities is the zero value, which supports nothing.
func (absent) Capabilities() Capabilities { return Capabilities{} }

// Request refuses. A consumer that asked Capabilities first never gets here.
func (absent) Request(context.Context, string, any, any) error { return ErrNoServer }

// Notify is DISCARDED rather than refused. A notification has no answer, so
// every document-synchronisation call in the editor would otherwise have to
// branch on whether a server exists — which is the branch this type removes.
func (absent) Notify(context.Context, string, any) error { return nil }

// DidOpen is discarded, for the reason Notify is.
func (absent) DidOpen(context.Context, Document) error { return nil }

// DidChange is discarded, for the reason Notify is.
func (absent) DidChange(context.Context, string, int, []Change) error { return nil }

// DidSave is discarded, for the reason Notify is.
func (absent) DidSave(context.Context, string, string) error { return nil }

// DidClose is discarded, for the reason Notify is.
func (absent) DidClose(context.Context, string) error { return nil }

// Stop succeeds, because there is nothing to stop.
func (absent) Stop(context.Context) error { return nil }
