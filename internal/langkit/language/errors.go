package language

import "errors"

// The errors a consumer of a Client must distinguish.
//
// They are sentinels rather than strings because the caller BRANCHES on them:
// ErrNotRunning means try again later or run without the feature, where
// ErrUnsupported means this server will never do this and asking again is
// pointless.
var (
	// ErrNoServer is answered by Absent and by a Servers router that has
	// nothing registered for a path. It is the ordinary state of an editor on
	// a machine with no language server installed, not a failure.
	ErrNoServer = errors.New("language: no server for this file")

	// ErrNotRunning is a server that exists but is not up — starting, crashed,
	// or given up on. The distinction from ErrNoServer matters to a status
	// bar: one is a machine without gopls, the other is gopls falling over.
	ErrNotRunning = errors.New("language: server is not running")

	// ErrUnsupported is a capability the server did not advertise. Reaching it
	// means the caller did not ask Capabilities first.
	ErrUnsupported = errors.New("language: server does not support this")

	// ErrFullSyncRequired is a ranged change offered to a server that
	// negotiated whole-document sync.
	ErrFullSyncRequired = errors.New("language: server requires whole-document changes")

	// ErrNotOpen is a change, save or close for a document that was never
	// opened. Sending one desynchronises the server's idea of the file from
	// the editor's, which surfaces later as diagnostics on lines that no
	// longer exist.
	ErrNotOpen = errors.New("language: document is not open")

	// ErrStaleVersion is a document version that did not advance. Versions are
	// how a server discards an edit that overtook another one.
	ErrStaleVersion = errors.New("language: document version did not advance")

	// ErrOutsideRoot is a path a server named that lies outside the workspace.
	// See Confined.
	ErrOutsideRoot = errors.New("language: path is outside the workspace root")
)

// The errors applying a server's edits can produce.
//
// Both are SERVER faults rather than the operator's, and both are refused
// rather than worked around: an edit applied to the wrong place and reported as
// success is worse than one that did not run, because the operator has no
// reason to look.
var (
	// ErrEditOutOfRange is an edit naming a position the document does not
	// have. Clamping it would write somewhere else and say nothing.
	ErrEditOutOfRange = errors.New("language: an edit is outside the document")

	// ErrOverlappingEdits is two edits in one set touching the same text. The
	// protocol forbids it, and splicing them anyway produces a file nobody
	// wrote.
	ErrOverlappingEdits = errors.New("language: edits overlap")

	// ErrRefused is a rename or a code action the editor would not carry out —
	// an edit naming a file outside the workspace is the case this exists for.
	ErrRefused = errors.New("language: the change was refused")
)
