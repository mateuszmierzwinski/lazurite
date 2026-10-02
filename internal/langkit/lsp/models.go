package lsp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// The JSON-RPC 2.0 envelopes.
//
// Four named shapes rather than one permissive struct, because they are read
// and written in different directions and conflating them is how a response
// gets sent where a notification belongs. `incoming` is the only permissive
// one, and it is permissive because a server may send any of the three.

// wireRequest is a request going out: it has an id and expects an answer.
type wireRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// wireNotification is a message going out that has no answer.
//
// The absence of an id is the whole difference, and it is load-bearing: a
// notification sent with one leaves the server waiting to reply to something
// nothing is listening for.
type wireNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// wireResponse is our answer to a request the SERVER made.
//
// The id is copied back verbatim as raw JSON because the protocol allows a
// number or a string and re-encoding it risks changing which.
type wireResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *wireError      `json:"error,omitempty"`
}

// incoming is anything arriving from the server.
//
// Which of the three it is comes from which fields are present: an id with a
// method is a request, an id alone is a response, a method alone is a
// notification. See classify.
type incoming struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *wireError      `json:"error,omitempty"`
}

// wireError is the protocol's error object.
type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// The JSON-RPC error codes this client sends. A server request naming a method
// we did not register for is answered rather than ignored: a server left
// waiting on an answer stalls whatever it was doing on our behalf.
const (
	codeMethodNotFound = -32601
	codeInternalError  = -32603
)

// jsonRPCVersion is the only version this client speaks, and the protocol
// requires it on every message.
const jsonRPCVersion = "2.0"

// initializeParams is the handshake's opening message.
type initializeParams struct {
	ProcessID    int                `json:"processId"`
	ClientInfo   clientInfo         `json:"clientInfo"`
	RootURI      string             `json:"rootUri"`
	Capabilities clientCapabilities `json:"capabilities"`
	Workspace    []workspaceFolder  `json:"workspaceFolders"`
}

// clientInfo names this editor to the server. Some servers change behaviour on
// it, and one that will not say who it is invites being treated as the
// lowest common denominator.
type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// workspaceFolder is one root the server should index.
type workspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// initializeResult is what the server answers with.
type initializeResult struct {
	Capabilities json.RawMessage `json:"capabilities"`
	ServerInfo   clientInfo      `json:"serverInfo"`
}

// clientCapabilities is what THIS client can honour.
//
// Deliberately not the whole protocol. A capability declared here is a promise
// the server may act on — it will send richer answers, register dynamically, or
// ask us to apply an edit — so declaring one the editor cannot handle produces
// a server doing work nobody reads, or worse, waiting on us.
//
// T5.2 to T5.4: ADD YOUR CAPABILITY HERE when you implement it. Semantic tokens
// (needs a legend negotiated at initialize) and `workspace.applyEdit` (needs
// somewhere to apply a workspace edit) are the two conspicuous absentees, and
// they belong to T5.3 and T5.4 respectively.
type clientCapabilities struct {
	TextDocument textDocumentCapabilities `json:"textDocument"`
	Workspace    workspaceCapabilities    `json:"workspace"`
}

// workspaceCapabilities is what we support at the project level.
type workspaceCapabilities struct {
	WorkspaceFolders bool                    `json:"workspaceFolders"`
	WorkspaceEdit    workspaceEditCapability `json:"workspaceEdit"`
	// ApplyEdit is FALSE on purpose: a server told we accept edits will send
	// them, and nothing here applies one yet.
	ApplyEdit bool `json:"applyEdit"`
}

// textDocumentCapabilities is what we support per file.
type textDocumentCapabilities struct {
	Synchronization    syncCapability       `json:"synchronization"`
	PublishDiagnostics toggle               `json:"publishDiagnostics"`
	Hover              contentCapability    `json:"hover"`
	Completion         completionCapability `json:"completion"`
	SignatureHelp      contentCapability    `json:"signatureHelp"`
	Definition         linkCapability       `json:"definition"`
	TypeDefinition     linkCapability       `json:"typeDefinition"`
	Implementation     linkCapability       `json:"implementation"`
	References         toggle               `json:"references"`
	DocumentHighlight  toggle               `json:"documentHighlight"`
	DocumentSymbol     toggle               `json:"documentSymbol"`
	CodeAction         codeActionCapability `json:"codeAction"`
	CodeLens           toggle               `json:"codeLens"`
	InlayHint          toggle               `json:"inlayHint"`
	CallHierarchy      toggle               `json:"callHierarchy"`
	Formatting         toggle               `json:"formatting"`
	RangeFormatting    toggle               `json:"rangeFormatting"`
	Rename             renameCapability     `json:"rename"`
}

// toggle is the shape most capabilities take.
//
// DynamicRegistration is false everywhere: honouring it means handling
// `client/registerCapability` as a real registration rather than an
// acknowledgement, which is a feature nothing needs yet.
type toggle struct {
	DynamicRegistration bool `json:"dynamicRegistration"`
}

// syncCapability is which document notifications we send.
//
// WillSave and WillSaveWaitUntil are false: the second one lets a server delay
// a save while it computes edits, and an editor that granted it would block a
// Ctrl+S on a subprocess.
type syncCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration"`
	DidSave             bool `json:"didSave"`
	WillSave            bool `json:"willSave"`
	WillSaveWaitUntil   bool `json:"willSaveWaitUntil"`
}

// contentCapability is a capability whose answer carries prose.
//
// The formats are in preference order, and markdown is first because
// `widgets.Markdown` renders it — this is the toolkit's own widget set
// deciding what the server should send.
type contentCapability struct {
	DynamicRegistration bool     `json:"dynamicRegistration"`
	ContentFormat       []string `json:"contentFormat,omitempty"`
}

// completionCapability says what a completion item may contain.
type completionCapability struct {
	DynamicRegistration bool           `json:"dynamicRegistration"`
	CompletionItem      completionItem `json:"completionItem"`
}

// completionItem is which item features the editor can draw.
//
// Snippet support is FALSE: a snippet is a template with tab stops, and
// inserting one as literal text puts `${1:name}` in the operator's file.
type completionItem struct {
	SnippetSupport       bool     `json:"snippetSupport"`
	DocumentationFormat  []string `json:"documentationFormat,omitempty"`
	DeprecatedSupport    bool     `json:"deprecatedSupport"`
	InsertReplaceSupport bool     `json:"insertReplaceSupport"`
	LabelDetailsSupport  bool     `json:"labelDetailsSupport"`
}

// linkCapability says this client can read the LocationLink form of a
// "where is it" answer.
//
// DECLARED because it is honoured: `linkLocations` reads the shape, and prefers
// the selection range in it — which is what makes a go-to-definition land on
// the symbol's name rather than on the first line of its declaration. A client
// that declared this and decoded only the older forms would answer "no
// definition found" against every server that took it at its word.
type linkCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration"`
	LinkSupport         bool `json:"linkSupport"`
}

// codeActionCapability says what this client can do with a code action.
//
// The LITERAL support is the important half: without it a server may answer
// with `Command` objects, whose work happens server-side and comes back as a
// `workspace/applyEdit` — which this client refuses (requests.go). Declaring
// literal support asks for actions that carry their own edit, which is the only
// kind this editor can actually apply.
//
// The kinds are the ones the editor offers: quick fixes, refactorings, and the
// source actions behind Organise Imports.
//
// DATA and RESOLVE are the other half of the same bargain: a server is allowed
// to answer with the NAME of a change and a token, and to work out what it does
// when the operator picks it. gopls does exactly that for every refactoring,
// and a client that cannot say so is offered them as commands it must drop.
type codeActionCapability struct {
	DynamicRegistration bool               `json:"dynamicRegistration"`
	CodeActionLiteral   codeActionLiteral  `json:"codeActionLiteralSupport"`
	IsPreferredSupport  bool               `json:"isPreferredSupport"`
	DisabledSupport     bool               `json:"disabledSupport"`
	DataSupport         bool               `json:"dataSupport"`
	ResolveSupport      *codeActionResolve `json:"resolveSupport,omitempty"`
}

// codeActionLiteral is which kinds of action the editor understands.
type codeActionLiteral struct {
	CodeActionKind codeActionKinds `json:"codeActionKind"`
}

// codeActionKinds is the value set, which the protocol requires.
type codeActionKinds struct {
	ValueSet []string `json:"valueSet"`
}

// codeActionResolve is which parts of an action this client will ask for later.
//
// "edit" and only "edit": the title and the kind are what the menu draws and
// must arrive with the offer, and an editor that let a server fill in the TITLE
// afterwards would be drawing a name for a change that was chosen under a
// different one.
type codeActionResolve struct {
	Properties []string `json:"properties"`
}

// workspaceEditCapability is what this client can apply.
//
// documentChanges is TRUE because `documentChangesOf` reads that form, which is
// what gopls answers a rename with. Resource operations are deliberately NOT
// declared: this editor changes text, and a server that created, renamed or
// deleted a file on the strength of a rename would be doing something the
// operator did not agree to. An undeclared operation is one a server should not
// send, and one sent anyway is skipped rather than performed.
type workspaceEditCapability struct {
	DocumentChanges bool `json:"documentChanges"`
}

// renameCapability asks the server for the prepare step, which is what makes a
// rename preview possible before anything is changed.
type renameCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration"`
	PrepareSupport      bool `json:"prepareSupport"`
}

// serverCapabilities is the initialize result's capability object.
//
// Most providers are `boolean | object` on the wire, so they are held as raw
// JSON and read by `provided`. Decoding them as bool works until the first
// server that answers with options, at which point every feature it offers
// silently disappears.
type serverCapabilities struct {
	TextDocumentSync  json.RawMessage  `json:"textDocumentSync"`
	Hover             json.RawMessage  `json:"hoverProvider"`
	Definition        json.RawMessage  `json:"definitionProvider"`
	TypeDefinition    json.RawMessage  `json:"typeDefinitionProvider"`
	Implementation    json.RawMessage  `json:"implementationProvider"`
	References        json.RawMessage  `json:"referencesProvider"`
	DocumentHighlight json.RawMessage  `json:"documentHighlightProvider"`
	DocumentSymbol    json.RawMessage  `json:"documentSymbolProvider"`
	WorkspaceSymbol   json.RawMessage  `json:"workspaceSymbolProvider"`
	CodeAction        json.RawMessage  `json:"codeActionProvider"`
	CodeLens          json.RawMessage  `json:"codeLensProvider"`
	InlayHint         json.RawMessage  `json:"inlayHintProvider"`
	SemanticTokens    json.RawMessage  `json:"semanticTokensProvider"`
	CallHierarchy     json.RawMessage  `json:"callHierarchyProvider"`
	TypeHierarchy     json.RawMessage  `json:"typeHierarchyProvider"`
	Formatting        json.RawMessage  `json:"documentFormattingProvider"`
	RangeFormatting   json.RawMessage  `json:"documentRangeFormattingProvider"`
	Rename            json.RawMessage  `json:"renameProvider"`
	Completion        *triggerProvider `json:"completionProvider"`
	SignatureHelp     *triggerProvider `json:"signatureHelpProvider"`
}

// codeActionOptions is the object form of `codeActionProvider`, which is where
// a server says it will resolve an action's edit on request.
type codeActionOptions struct {
	ResolveProvider bool `json:"resolveProvider"`
}

// triggerProvider is a provider that fires on particular characters.
//
// ResolveProvider is completion's alone: it says the server will fill in an
// item's documentation and edits when asked, which is a SECOND capability and
// not implied by offering completion at all.
type triggerProvider struct {
	TriggerCharacters []string `json:"triggerCharacters"`
	ResolveProvider   bool     `json:"resolveProvider"`
}

// syncOptions is the object form of textDocumentSync.
type syncOptions struct {
	OpenClose bool            `json:"openClose"`
	Change    int             `json:"change"`
	Save      json.RawMessage `json:"save"`
}

// saveOptions is the object form of textDocumentSync.save.
type saveOptions struct {
	IncludeText bool `json:"includeText"`
}

// syncSupport is what the sync capability amounts to, once the number form and
// the object form have been reconciled.
type syncSupport struct {
	Kind        int
	OpenClose   bool
	Save        bool
	IncludeText bool
}

// renameOptions is the object form of renameProvider.
type renameOptions struct {
	Prepare bool `json:"prepareProvider"`
}

// The document-synchronisation parameter shapes.

// textDocumentIdentifier names a document by URI.
type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

// versionedIdentifier names a document and which edit of it this is.
type versionedIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// textDocumentItem is a document being opened, text and all.
type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// didOpenParams announces a document.
type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

// didChangeParams carries one or more edits.
type didChangeParams struct {
	TextDocument versionedIdentifier `json:"textDocument"`
	Changes      []contentChange     `json:"contentChanges"`
}

// contentChange is one edit. An absent range replaces the whole document,
// which is what the protocol means by full synchronisation.
type contentChange struct {
	Range *wireRange `json:"range,omitempty"`
	Text  string     `json:"text"`
}

// didSaveParams reports a save, with the text where the server asked for it.
type didSaveParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Text         string                 `json:"text,omitempty"`
}

// didCloseParams tells the server to forget a document.
type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

// cancelParams withdraws a request whose caller has gone.
type cancelParams struct {
	ID int64 `json:"id"`
}

// wirePosition and wireRange are the protocol's spellings of the core types.
type wirePosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// wireRange is a span on the wire.
type wireRange struct {
	Start wirePosition `json:"start"`
	End   wirePosition `json:"end"`
}

// openDocument is what this client remembers about a file it opened.
//
// The TEXT is deliberately not here. The operator's buffer lives in the editor
// widget, and a second copy that drifts from it is worse than asking the caller
// for text it already has — which is why a whole-document-sync server is served
// by requiring the caller to supply a full change (language.ErrFullSyncRequired).
type openDocument struct {
	path       string
	languageID string
	version    int
}

// initializedParams is the empty object the `initialized` notification carries.
// The protocol requires the object; a notification with no params at all is
// rejected by some servers.
type initializedParams struct{}

// Deps is everything a client needs, supplied by the composition root.
type Deps struct {
	// Spec is which server to run and what it handles.
	Spec language.ServerSpec

	// Launcher starts it. Nil takes the process launcher, which is what the
	// composition root wants and what a test replaces.
	Launcher Launcher

	// Sink receives diagnostics and everything else the server says unasked.
	// Nil discards them.
	Sink language.Sink

	// Reporter is how a missing or crashed server reaches the operator. Nil
	// sends the message to stderr, which is where a program with no window yet
	// can still be read.
	Reporter language.Reporter

	// Reopener re-announces the open documents after a restart. Nil means a
	// restarted server is told about no files — see language.Reopener.
	Reopener language.Reopener

	// Lifetime is the WINDOW's context, threaded from the composition root
	// (Rule 20.5). It bounds the server process and the supervisor goroutine:
	// when the window ends, so do they. Nil is treated as a context that is
	// never done, which is correct only in a test.
	Lifetime context.Context

	// RequestTimeout bounds a request whose caller supplied no deadline of its
	// own. Zero takes defaultRequestTimeout. "The server will answer
	// eventually" is a claim, and this is where it stops being taken on trust.
	RequestTimeout time.Duration

	// HandshakeTimeout bounds initialize. Zero takes defaultHandshakeTimeout.
	// A server that has not answered by then is a server that is not there.
	HandshakeTimeout time.Duration

	// MaxRestarts is how many times a crashed server is restarted before the
	// editor gives up and runs without language features. Zero takes
	// defaultMaxRestarts; a negative number never restarts.
	MaxRestarts int
}

// docSync is what the negotiated capabilities mean for document notifications.
//
// A small copy taken under the lock rather than the whole Capabilities struct,
// because these four are read on the didChange path — which runs at typing
// speed — and the rest of that struct is answers to questions nobody is asking
// while somebody types.
type docSync struct {
	kind        language.SyncKind
	openClose   bool
	save        bool
	includeText bool
}

// publishParams is a `textDocument/publishDiagnostics` notification.
//
// Version is a plain int rather than a pointer: the protocol makes it optional
// and its absence decodes as zero, which is not a document version any editor
// ever assigns — so the zero value already means "the server did not say".
type publishParams struct {
	URI         string           `json:"uri"`
	Version     int              `json:"version"`
	Diagnostics []wireDiagnostic `json:"diagnostics"`
}

// wireDiagnostic is one problem as the server states it.
//
// Code is raw JSON because the protocol allows a number OR a string, and a
// struct that picked one silently drops every diagnostic from a server that
// chose the other.
type wireDiagnostic struct {
	Range    wireRange       `json:"range"`
	Severity int             `json:"severity"`
	Code     json.RawMessage `json:"code"`
	Source   string          `json:"source"`
	Message  string          `json:"message"`
}
