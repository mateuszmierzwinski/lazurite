package language

// Feature is one thing a language server can be asked.
//
// A NAMED question rather than a method per operation, because the window asks
// them all the same way — from a background goroutine, with a deadline, and
// with the answer posted back — and thirteen ports differing only in their
// result type would be thirteen copies of that machinery.
type Feature int

// The questions this editor asks.
//
// The order is the order they were built in: navigation and information first
// (T5.3), then refactoring and formatting (T5.4).
const (
	// FeatureDefinition is where a symbol is declared.
	FeatureDefinition Feature = iota

	// FeatureTypeDefinition is where the TYPE of the symbol under the caret is
	// declared, which is a different question and a different answer.
	FeatureTypeDefinition

	// FeatureImplementation is what satisfies an interface.
	FeatureImplementation

	// FeatureReferences is everywhere a symbol is used, its declaration
	// included.
	FeatureReferences

	// FeatureHover is what the symbol under the caret is, as prose.
	FeatureHover

	// FeatureSignature is the parameters of the call being typed.
	FeatureSignature

	// FeatureHighlight is every occurrence of the symbol under the caret in
	// THIS file, which is what makes a name light up as the caret enters it.
	FeatureHighlight

	// FeaturePrepareRename asks whether the symbol under the caret can be
	// renamed at all, and what it is currently called.
	FeaturePrepareRename

	// FeatureRename is the edits that rename a symbol across the workspace.
	FeatureRename

	// FeatureFormat is the edits that format a whole document.
	FeatureFormat

	// FeatureFormatRange is the edits that format a selection.
	FeatureFormatRange

	// FeatureCodeAction is what the server offers to do about the caret's
	// position — a quick fix, a refactoring.
	FeatureCodeAction

	// FeatureOrganiseImports is the import block put in order, which is a code
	// action of a particular kind rather than a request of its own.
	FeatureOrganiseImports

	// FeatureCompletion is what may be typed at the caret.
	FeatureCompletion

	// FeatureCompletionResolve fills in the ONE item the operator is looking
	// at: its documentation, and the import that goes in with it. A feature of
	// its own because it is a separate request against a separate capability —
	// a server may offer completion and not offer resolve, and asking anyway
	// reaches the operator as a protocol error number.
	FeatureCompletionResolve

	// FeatureCodeActionResolve fills in the ONE action the operator chose: the
	// edit the server kept back.
	//
	// A feature of its own for the reason completion's resolve is one — it is a
	// separate request against a separate capability — and a necessary one
	// rather than an optimisation. gopls answers a refactoring with a title and
	// an opaque token and computes the change only when it is asked to, because
	// working out what "extract this into a function" does costs a type-check
	// of the package. A client that did not ask is told about a refactoring it
	// can never carry out.
	FeatureCodeActionResolve

	// The two hierarchies, three features each: the PREPARE that turns a
	// position into something the server will answer about, and the two
	// directions it can then be read in (hierarchy.go).
	FeaturePrepareCallHierarchy
	FeatureIncomingCalls
	FeatureOutgoingCalls
	FeaturePrepareTypeHierarchy
	FeatureSupertypes
	FeatureSubtypes
)

// String names a feature, for a message the operator reads.
func (f Feature) String() string {
	if name, known := featureNames[f]; known {
		return name
	}
	return "unknown"
}

// featureNames is what each feature is called in a report.
//
// Package-scoped and built once (Rule 6.2). Prose rather than the protocol's
// method names: a status bar saying "textDocument/typeDefinition is not
// supported" is telling the operator about a wire format.
var featureNames = map[Feature]string{
	FeatureDefinition:      "go to definition",
	FeatureTypeDefinition:  "go to type definition",
	FeatureImplementation:  "go to implementation",
	FeatureReferences:      "find references",
	FeatureHover:           "hover",
	FeatureSignature:       "signature help",
	FeatureHighlight:       "highlight occurrences",
	FeaturePrepareRename:   "rename",
	FeatureRename:          "rename",
	FeatureFormat:          "format document",
	FeatureFormatRange:     "format selection",
	FeatureCodeAction:      "code actions",
	FeatureOrganiseImports: "organise imports",
	// Both completion requests are named "completion": the operator asked for
	// one thing, and "completionItem/resolve is not supported" would be this
	// editor explaining its own request sequence to somebody typing.
	FeatureCompletion:        "completion",
	FeatureCompletionResolve: "completion",
	// Likewise the two halves of a code action: the operator asked for one
	// thing, and a message naming `codeAction/resolve` is this editor
	// explaining its own request sequence to somebody who pressed a key.
	FeatureCodeActionResolve: "code actions",
	// The hierarchies are named by the TREE rather than by the step, for the
	// reason completion's two are named by the feature: the operator asked to
	// see the callers of something, and "callHierarchy/incomingCalls is not
	// supported" is this editor explaining its request sequence to somebody
	// who pressed a key.
	FeaturePrepareCallHierarchy: callHierarchyName,
	FeatureIncomingCalls:        callHierarchyName,
	FeatureOutgoingCalls:        callHierarchyName,
	FeaturePrepareTypeHierarchy: typeHierarchyName,
	FeatureSupertypes:           typeHierarchyName,
	FeatureSubtypes:             typeHierarchyName,
}

// The two trees' names, each shared by the three requests that make it up: the
// operator asked to see one thing, and three names for it in the status bar
// would be this editor describing its own request sequence.
const (
	callHierarchyName = "call hierarchy"
	typeHierarchyName = "type hierarchy"
)

// Query is one question about one place in one file.
//
// A single shape for every feature, with the fields a particular one needs. The
// alternative is thirteen parameter structs, twelve of which are a path and a
// position.
type Query struct {
	// Feature is what is being asked.
	Feature Feature

	// Path is the file, and Position the point in it. Both are required for
	// every feature: a document-wide format is asked at the top of the file.
	Path     string
	Position Position

	// Span is the selection, for formatting a range and for asking what code
	// actions apply to it. The zero value is the caret's own line.
	Span Range

	// NewName is what a rename is renaming TO.
	NewName string

	// Options is how the operator's editor is set up, for a formatter that
	// honours it.
	Options FormatOptions

	// Diagnostics are the problems the code actions should address, which is
	// what makes a quick fix offer to fix THIS error rather than list every
	// refactoring the server knows.
	Diagnostics []Diagnostic

	// Item is the completion being RESOLVED — the one row the operator is
	// looking at. It is sent back to the server as it arrived, opaque token
	// and all, because resolve is the server being handed its own item.
	Item Completion

	// Action is the code action being RESOLVED — the one the operator chose.
	// It goes back to the server as it arrived, opaque token and all, for the
	// same reason a completion item does: resolve is the server being handed
	// its own offer.
	Action CodeAction

	// Node is the hierarchy item being asked ABOUT — the function whose callers
	// are wanted, the type whose implementations are. It goes back to the
	// server as it arrived, opaque token and all, because a node three levels
	// down names a place this window has never opened (hierarchy.go).
	Node HierarchyItem

	// Only narrows a code-action request to particular KINDS — the protocol's
	// own `refactor.extract`, `refactor.inline` and their neighbours.
	//
	// It is what makes a refactoring a first-class action rather than an entry
	// to hunt for in a menu of everything the server knows: the window asks for
	// the one kind the operator named, and a server that cannot do it here
	// answers with nothing rather than with something else.
	Only []string

	// Trigger is what opened a completion list: the character that triggered
	// it, or empty where the operator simply typed a name. A server ranks
	// differently after a dot than it does mid-identifier, so this is not
	// decoration.
	Trigger string
}

// Answer is what came back.
//
// One shape with a field per result kind rather than an `any`: a caller reads
// the field its own feature fills and the compiler still knows the type, which
// a type switch on `any` gives up.
//
// Err is the only field every caller must read. A refusal — no server, an
// unsupported feature, a deadline — is an answer like any other and is
// delivered the same way, so a consumer has ONE place to handle it rather than
// one per call site.
type Answer struct {
	Feature    Feature
	Locations  []Location
	Hover      Hover
	Signatures SignatureHelp
	Highlights []Highlight
	Rename     PrepareRename

	// Edits are changes to ONE file — what a formatter answers with.
	Edits []TextEdit

	// Edit is changes across the workspace — what a rename and a code action
	// answer with. Kept apart from Edits because the two are applied
	// differently: one is a buffer, the other is files on disk.
	Edit WorkspaceEdit

	// Actions are what a server offers to do, and Action is one of them with
	// its edit filled in. Separate fields because they answer separate
	// requests, and because a resolve that overwrote the list would throw away
	// the menu the operator is choosing from.
	Actions []CodeAction
	Action  CodeAction

	// Roots are what a prepare answered with — usually one item, occasionally
	// several where a position names more than one thing — and Edges are one
	// level of a hierarchy: the nodes, each with the places it is used from.
	Roots []HierarchyItem
	Edges []HierarchyEdge

	// Completions are what may be typed at the caret, and Item is one of them
	// with its documentation and edits filled in. Separate fields because they
	// answer separate requests, and a resolve that overwrote the list would
	// throw away the twelve rows the operator is looking at to redraw one.
	Completions CompletionList
	Item        Completion

	Err error
}

// Hover is what the symbol under the caret is, as prose.
//
// The text is MARKDOWN, which is what this client asks for and what
// `widgets.Markdown` renders. It has been through CleanText: it is a
// subprocess's prose about to be drawn over the operator's code.
type Hover struct {
	Text string

	// Span is what the answer is ABOUT, so a reader can be shown which part of
	// the line the prose describes. HasSpan is false where the server did not
	// say, which is common and not an error.
	Span    Range
	HasSpan bool
}

// SignatureHelp is the call being typed.
type SignatureHelp struct {
	Signatures []Signature

	// Active is which signature applies, and Parameter which argument the
	// caret is in. Both are indices into what the server sent, already clamped
	// — an index past the end of the list is a server's arithmetic error and
	// must not become the editor's panic.
	Active    int
	Parameter int
}

// Signature is one callable form: its label, its documentation and the spans
// of its parameters within the label.
type Signature struct {
	Label         string
	Documentation string
	Parameters    []string
}

// HighlightKind says what an occurrence DOES with the symbol. The values are
// the protocol's.
type HighlightKind int

// The three kinds of occurrence.
const (
	// HighlightText is a mention with no read or write behind it — a comment,
	// a string.
	HighlightText HighlightKind = 1

	// HighlightRead is the symbol being used.
	HighlightRead HighlightKind = 2

	// HighlightWrite is the symbol being assigned to, which is the one worth
	// telling apart: "where is this changed" is the question that sends
	// somebody looking through a file.
	HighlightWrite HighlightKind = 3
)

// Highlight is one occurrence of the symbol under the caret, in this file.
type Highlight struct {
	Span Range
	Kind HighlightKind
}

// TextEdit is one replacement in one file.
//
// The protocol's own shape. Edits in a set are against the ORIGINAL text and
// must not overlap, which is why applying them runs back to front — see Apply.
type TextEdit struct {
	Span    Range
	NewText string
}

// FileEdits is every edit to one file.
type FileEdits struct {
	Path  string
	Edits []TextEdit
}

// WorkspaceEdit is a change that may span files, which is what a rename is.
//
// A SLICE rather than a map, because the order the server gave is the order the
// preview lists them in and a map's is whatever the runtime felt like — a
// preview that reordered itself between two viewings is one nobody trusts.
type WorkspaceEdit struct {
	Files []FileEdits
}

// Empty reports whether an edit changes nothing, which is what a rename to the
// name a symbol already has answers with.
func (e WorkspaceEdit) Empty() bool {
	for _, file := range e.Files {
		if len(file.Edits) > 0 {
			return false
		}
	}
	return true
}

// CodeAction is something the server offers to do.
//
// Title is a subprocess's prose and has been scrubbed; it is what the operator
// reads before agreeing to a change, so it may not carry a control code or a
// reordering override.
type CodeAction struct {
	Title string

	// Kind is the protocol's category — "quickfix", "source.organizeImports".
	// It is what lets a menu group them and what makes organise-imports a code
	// action rather than a request of its own.
	Kind string

	// Preferred marks the one the server would choose, which is what a "fix
	// this" key applies without asking.
	Preferred bool

	// Edit is what the action does, where the server sent it. An action with an
	// empty edit and a Data token is one the server has KEPT BACK and will
	// compute when asked (FeatureCodeActionResolve); one with neither is an
	// action whose work is a server-side command, and this editor does not run
	// those — see the applyEdit refusal in the adapter.
	Edit WorkspaceEdit

	// Data is the server's opaque token for this action, handed back verbatim
	// on resolve. It is never read here: it belongs to the server.
	Data []byte
}

// NeedsResolve reports whether the server kept this action's change back.
//
// The question a caller asks before applying one: an action with a token and no
// edit is not an empty change, it is a change nobody has asked for yet.
func (a CodeAction) NeedsResolve() bool { return a.Edit.Empty() && len(a.Data) > 0 }

// PrepareRename is whether a symbol may be renamed, and what it is called now.
//
// Asked before a rename so the operator is offered a field with the current
// name in it, and so a caret on a keyword is refused BEFORE they have typed a
// new name for it.
type PrepareRename struct {
	Allowed     bool
	Span        Range
	Placeholder string
}

// FormatOptions is how the operator's editor is set up.
//
// Sent so a formatter that honours it produces what the editor draws. Most
// language formatters have opinions of their own and ignore this, which is
// their business and not a reason to lie about the settings.
type FormatOptions struct {
	TabSize      int
	InsertSpaces bool
}
