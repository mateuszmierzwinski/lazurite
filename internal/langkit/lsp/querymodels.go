package lsp

import "encoding/json"

// The request and answer shapes for the questions T5.3 and T5.4 ask.
//
// They live here, with the rest of the protocol, for the reason the
// document-synchronisation shapes do: nothing above this package knows that
// JSON-RPC exists, and a window that had to name `textDocument/hover` would be
// a window that had learned the protocol.

// The methods asked. Named constants rather than literals at the call site,
// because a typo in one of these is a `method not found` at runtime that reads
// as the server not supporting the feature.
const (
	methodDefinition      = "textDocument/definition"
	methodTypeDefinition  = "textDocument/typeDefinition"
	methodImplementation  = "textDocument/implementation"
	methodReferences      = "textDocument/references"
	methodHover           = "textDocument/hover"
	methodSignatureHelp   = "textDocument/signatureHelp"
	methodHighlight       = "textDocument/documentHighlight"
	methodPrepareRename   = "textDocument/prepareRename"
	methodRename          = "textDocument/rename"
	methodFormatting      = "textDocument/formatting"
	methodRangeFormatting = "textDocument/rangeFormatting"
	methodCodeAction      = "textDocument/codeAction"

	// Resolve is about an ACTION rather than a place, which is why it is not a
	// `textDocument/` request.
	methodCodeActionResolve = "codeAction/resolve"
)

// positionParams is a document and a point in it, which is what every
// navigation request takes.
type positionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     wirePosition           `json:"position"`
}

// referenceParams adds whether the declaration itself is wanted.
//
// It IS wanted. "Find references" that omitted the declaration would answer a
// different question from the one every editor's menu entry implies.
type referenceParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     wirePosition           `json:"position"`
	Context      referenceContext       `json:"context"`
}

// referenceContext is the reference request's one option.
type referenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// renameParams is a symbol and what it is being renamed to.
type renameParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     wirePosition           `json:"position"`
	NewName      string                 `json:"newName"`
}

// formattingParams is a whole document and how the editor is set up.
type formattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Options      wireFormatOptions      `json:"options"`
}

// rangeFormattingParams is a selection and the same options.
type rangeFormattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        wireRange              `json:"range"`
	Options      wireFormatOptions      `json:"options"`
}

// wireFormatOptions is the protocol's spelling of the editor's settings.
type wireFormatOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

// codeActionParams asks what can be done about a span.
type codeActionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        wireRange              `json:"range"`
	Context      codeActionContext      `json:"context"`
}

// codeActionContext narrows what is offered.
//
// The DIAGNOSTICS are what make a quick fix offer to fix this error rather than
// list every refactoring the server knows, and Only is what makes organise
// imports a single answer rather than a menu to search.
type codeActionContext struct {
	Diagnostics []wireDiagnostic `json:"diagnostics"`
	Only        []string         `json:"only,omitempty"`
}

// wireLocation is a place in a file.
type wireLocation struct {
	URI   string    `json:"uri"`
	Range wireRange `json:"range"`
}

// wireLocationLink is the richer answer a server may give instead.
//
// The protocol allows `Location | Location[] | LocationLink[]`, and a client
// that decoded only the first two silently answers "no definition found"
// against a server that sends the third — which gopls does when the client
// declares link support and several do regardless.
type wireLocationLink struct {
	TargetURI            string    `json:"targetUri"`
	TargetRange          wireRange `json:"targetRange"`
	TargetSelectionRange wireRange `json:"targetSelectionRange"`
}

// wireHover is the hover answer.
//
// Contents is raw because the protocol has THREE historical shapes for it: a
// string, a `{language, value}` object, an array of either, and the current
// `{kind, value}`. A client that decoded one of them shows nothing against a
// server that sends another.
type wireHover struct {
	Contents json.RawMessage `json:"contents"`
	Range    *wireRange      `json:"range"`
}

// markupContent is the current shape of any prose the protocol carries.
type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// wireSignatureHelp is the call being typed.
type wireSignatureHelp struct {
	Signatures      []wireSignature `json:"signatures"`
	ActiveSignature int             `json:"activeSignature"`
	ActiveParameter int             `json:"activeParameter"`
}

// wireSignature is one callable form.
type wireSignature struct {
	Label         string              `json:"label"`
	Documentation json.RawMessage     `json:"documentation"`
	Parameters    []wireSignatureItem `json:"parameters"`
}

// wireSignatureItem is one parameter.
//
// Label is raw because the protocol allows a string OR a pair of offsets into
// the signature's own label, and the two need different handling.
type wireSignatureItem struct {
	Label json.RawMessage `json:"label"`
}

// wireHighlight is one occurrence in the open file.
type wireHighlight struct {
	Range wireRange `json:"range"`
	Kind  int       `json:"kind"`
}

// wireTextEdit is one replacement.
type wireTextEdit struct {
	Range   wireRange `json:"range"`
	NewText string    `json:"newText"`
}

// wireWorkspaceEdit is a change across files.
//
// BOTH forms are read. `changes` is a map of URI to edits; `documentChanges` is
// an ordered list that can also create, rename and delete files. A client that
// read only `changes` gets nothing from a server that answers with the other,
// which gopls does — and reading `documentChanges` is also what lets the file
// OPERATIONS be seen and refused rather than silently ignored.
type wireWorkspaceEdit struct {
	Changes         map[string][]wireTextEdit `json:"changes"`
	DocumentChanges []json.RawMessage         `json:"documentChanges"`
}

// wireDocumentEdit is one file's entry in documentChanges.
//
// A documentChanges entry is either this or a file OPERATION — a create, a
// rename or a delete, each identified by a `kind` field this shape does not
// have. TextDocument is a pointer so its absence can be told from an empty one.
type wireDocumentEdit struct {
	Kind         string               `json:"kind"`
	TextDocument *versionedIdentifier `json:"textDocument"`
	Edits        []wireTextEdit       `json:"edits"`
}

// wireCodeAction is one thing the server offers to do.
//
// Command is raw and only its PRESENCE is read: an action whose work is a
// server-side command is one this editor cannot carry out, because running it
// means accepting a `workspace/applyEdit` back and this client declares it does
// not (see requests.go).
type wireCodeAction struct {
	Title       string             `json:"title"`
	Kind        string             `json:"kind"`
	IsPreferred bool               `json:"isPreferred"`
	Edit        *wireWorkspaceEdit `json:"edit"`
	Command     json.RawMessage    `json:"command"`
	Disabled    *codeActionReason  `json:"disabled"`

	// Data is the server's own token for an action whose edit it has KEPT
	// BACK. It is opaque, it is echoed on resolve, and it is never read.
	Data json.RawMessage `json:"data"`
}

// resolveCodeAction is the action handed back to the server to be filled in.
//
// The three fields gopls needs and nothing else: an action is not a document,
// and sending back a decoded-and-re-encoded copy of everything that arrived
// would be this client inventing a shape the server never sent.
type resolveCodeAction struct {
	Title string          `json:"title"`
	Kind  string          `json:"kind,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// codeActionReason is why a server marked an action unavailable.
type codeActionReason struct {
	Reason string `json:"reason"`
}

// wirePrepareRename is the answer to prepareRename.
//
// The protocol allows a bare range, a `{range, placeholder}` object, or
// `{defaultBehavior}` — so it is decoded permissively and an answer that fits
// none of them is read as "this cannot be renamed", which is the safe
// direction.
type wirePrepareRename struct {
	Range           *wireRange   `json:"range"`
	Placeholder     string       `json:"placeholder"`
	DefaultBehavior bool         `json:"defaultBehavior"`
	Start           wirePosition `json:"start"`
	End             wirePosition `json:"end"`
}
