package language

// Position is a point in a document, zero-based.
//
// Character counts UTF-16 code units, which is the protocol's default and not
// a choice this package makes. A consumer converting to a rune offset must do
// that conversion; recording it here is what stops it being rediscovered.
type Position struct {
	Line      int
	Character int
}

// Range is a span between two positions. End is exclusive.
type Range struct {
	Start Position
	End   Position
}

// Location is a range in a named file.
//
// Path is a filesystem path, never a URI: the protocol's `file://` form is the
// adapter's business and a window that had to parse URIs would be a window
// that had learned the protocol.
type Location struct {
	Path  string
	Range Range
}

// Severity is how bad a diagnostic is. The values are the protocol's.
type Severity int

// The four severities, in the protocol's own order.
const (
	SeverityError   Severity = 1
	SeverityWarning Severity = 2
	SeverityInfo    Severity = 3
	SeverityHint    Severity = 4
)

// String names a severity for a problems panel.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	case SeverityHint:
		return "hint"
	}
	return "unknown"
}

// Diagnostic is one problem the server found.
type Diagnostic struct {
	Range    Range
	Severity Severity
	Code     string
	Source   string
	Message  string
}

// Notification is one unsolicited message from the server.
//
// Params is raw JSON, and is nil where the notification carried none. It is
// bytes rather than a decoded shape because this package does not know the
// protocol — see Sink.
type Notification struct {
	Method string
	Params []byte
}

// SyncKind is how the server wants document changes delivered. The values are
// the protocol's.
type SyncKind int

// The three sync kinds.
const (
	SyncNone        SyncKind = 0
	SyncFull        SyncKind = 1
	SyncIncremental SyncKind = 2
)

// Capabilities is what the server said it can do.
//
// The zero value supports NOTHING, which is what makes it safe as the answer
// from a client that never started: a consumer asking before calling gets a
// truthful no rather than a nil dereference.
//
// Raw is the untouched `capabilities` object from the initialize result. It is
// here so T5.2 to T5.4 can read a capability this struct does not name without
// editing this file first — a server's capability set is open-ended and the
// named fields are the ones the backlog's own rows need.
type Capabilities struct {
	Sync              SyncKind
	OpenClose         bool
	Save              bool
	SaveIncludesText  bool
	Hover             bool
	Definition        bool
	TypeDefinition    bool
	Implementation    bool
	References        bool
	DocumentHighlight bool
	DocumentSymbol    bool
	WorkspaceSymbol   bool
	Completion        bool
	CompletionResolve bool
	SignatureHelp     bool
	CodeAction        bool
	CodeActionResolve bool
	CodeLens          bool
	InlayHint         bool
	SemanticTokens    bool
	CallHierarchy     bool
	TypeHierarchy     bool
	Formatting        bool
	RangeFormatting   bool
	Rename            bool
	PrepareRename     bool
	CompletionTrigger []string
	SignatureTrigger  []string
	Raw               []byte
}

// Status is whether language features are available.
type Status int

// The four states a client can be in.
const (
	// StatusStopped is a client that has not been started, or one that was
	// stopped deliberately.
	StatusStopped Status = iota
	// StatusStarting is a launched server whose handshake has not finished.
	StatusStarting
	// StatusReady is a server that answered initialize.
	StatusReady
	// StatusFailed is a server that could not be launched, could not be
	// initialised, or crashed too often to be worth restarting. It is a
	// TERMINAL state and the editor runs with no language features.
	StatusFailed
)

// String names a status for a status bar.
func (s Status) String() string {
	switch s {
	case StatusStarting:
		return "starting"
	case StatusReady:
		return "ready"
	case StatusFailed:
		return "failed"
	}
	return "stopped"
}

// ServerSpec is how to start one language server.
//
// Command is an executable name or path, resolved through PATH, and Args are
// passed to it directly. Neither is ever handed to a shell: a command string
// carrying `;` must be an executable that does not exist, not two commands.
//
// Extensions are the file extensions this server handles, each with its
// leading dot and lower case (".go", ".rs"). Root is the workspace directory
// the server is told to open.
type ServerSpec struct {
	Name       string
	Command    string
	Args       []string
	Extensions []string
	Root       string
	Env        []string
}

// Document is a file the editor has open.
type Document struct {
	Path       string
	LanguageID string
	Version    int
	Text       string
}

// Change is one edit to an open document.
//
// A nil Range means the whole document is being replaced by Text, which is what
// a server negotiating SyncFull requires. A non-nil Range replaces just that
// span, which is what SyncIncremental allows.
type Change struct {
	Range *Range
	Text  string
}

// FullChange is the whole-document change every server accepts.
//
// A facade over the Change literal, so a caller that has the buffer's text and
// nothing else does not have to know that a nil range means "all of it".
func FullChange(text string) []Change { return []Change{{Text: text}} }

// Publication is one `textDocument/publishDiagnostics` notification, decoded.
//
// It REPLACES what was known about the file rather than adding to it, which is
// the protocol's own rule: a server that has fixed everything publishes an
// empty list, and a consumer that merged would go on showing errors that are
// no longer there.
//
// Version is the document version the diagnostics were computed against, and
// zero where the server did not say. A consumer may use it to drop an answer
// about text the operator has already typed past; it is not required to.
type Publication struct {
	Path        string
	Version     int
	Diagnostics []Diagnostic
}
