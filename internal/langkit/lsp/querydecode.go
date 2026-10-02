package lsp

import (
	"encoding/json"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// What a server's answers are decoded into, and the bounds they are decoded
// under.
//
// Every one of these reads JSON a subprocess wrote. Three rules hold throughout:
// a shape that does not decode is read as an EMPTY answer rather than an error,
// because the protocol has several historical forms for most of these and a
// client that refused the unfamiliar one shows nothing; every string a person
// will read goes through `language.CleanLine` or `CleanText`; and every list is
// bounded before it reaches a pane.

// The bounds. Far above any honest answer and far below what believing an
// unbounded one would cost.
const (
	// maxLocations bounds a references list. A symbol used ten thousand times
	// is real; one used a million times is a server fault, and the pane would
	// spend a frame's budget laying out rows nobody scrolls to.
	maxLocations = 10000

	// maxActions bounds a code-action menu, which a person reads.
	maxActions = 100

	// maxActionData bounds one action's opaque token. It is a handle the server
	// gave itself, not a document: gopls sends a few hundred bytes, and a
	// megabyte of it is a server this editor should stop believing rather than
	// store a hundred copies of.
	maxActionData = 64 * 1024

	// maxEdits bounds one file's edits, and maxEditFiles a workspace edit's
	// files. A rename across a monorepo genuinely touches hundreds of files.
	maxEdits     = 20000
	maxEditFiles = 2000

	// maxSignatures bounds an overload list.
	maxSignatures = 32
)

// locationsOf reads the three shapes a "where is it" answer may take.
//
// `Location | Location[] | LocationLink[]`, and null. A client that decoded
// only the first two answers "no definition found" against a server that sends
// the third — which is not a hypothetical: gopls sends links.
func locationsOf(raw json.RawMessage) []language.Location {
	if len(raw) == 0 {
		return nil
	}
	if one, ok := singleLocation(raw); ok {
		return []language.Location{one}
	}
	if many := manyLocations(raw); len(many) > 0 {
		return many
	}
	return linkLocations(raw)
}

// singleLocation reads the bare-object form.
func singleLocation(raw json.RawMessage) (language.Location, bool) {
	var one wireLocation
	if err := json.Unmarshal(raw, &one); err != nil || one.URI == "" {
		return language.Location{}, false
	}
	return locationOf(one)
}

// manyLocations reads the array-of-locations form.
func manyLocations(raw json.RawMessage) []language.Location {
	var list []wireLocation
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	out := make([]language.Location, 0, min(len(list), maxLocations))
	for _, item := range list[:min(len(list), maxLocations)] {
		if place, ok := locationOf(item); ok {
			out = append(out, place)
		}
	}
	return out
}

// linkLocations reads the LocationLink form.
//
// The SELECTION range is preferred where the server gave one: it is the symbol
// itself, where the target range is the whole declaration — and a jump that
// landed on the first line of a fifty-line function has taken the reader
// somewhere less useful than the name they asked about.
func linkLocations(raw json.RawMessage) []language.Location {
	var list []wireLocationLink
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil
	}
	out := make([]language.Location, 0, min(len(list), maxLocations))
	for _, link := range list[:min(len(list), maxLocations)] {
		path, err := uriToPath(link.TargetURI)
		if err != nil {
			continue
		}
		out = append(out, language.Location{Path: path, Range: rangeOf(selectionOf(link))})
	}
	return out
}

// selectionOf is the span a link should land on.
func selectionOf(link wireLocationLink) wireRange {
	if link.TargetSelectionRange != (wireRange{}) {
		return link.TargetSelectionRange
	}
	return link.TargetRange
}

// locationOf converts one place, refusing a URI that is not a file.
func locationOf(one wireLocation) (language.Location, bool) {
	path, err := uriToPath(one.URI)
	if err != nil {
		return language.Location{}, false
	}
	return language.Location{Path: path, Range: rangeOf(one.Range)}, true
}

// hoverOf reads a hover answer.
func hoverOf(answer *wireHover) language.Hover {
	if answer == nil {
		return language.Hover{}
	}
	out := language.Hover{Text: language.CleanText(proseOf(answer.Contents))}
	if answer.Range != nil {
		out.Span, out.HasSpan = rangeOf(*answer.Range), true
	}
	return out
}

// proseOf reads any of the four shapes the protocol carries prose in.
//
// A plain string, a `{kind, value}` MarkupContent, a `{language, value}`
// MarkedString, or an array of those. All four are live: the last two are
// deprecated and still sent, and a client that read only the current one shows
// an empty hover against a server that has not moved.
func proseOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	if single, ok := markupOf(raw); ok {
		return single
	}
	return joinedProse(raw)
}

// markupOf reads the object forms, which share a `value` field.
func markupOf(raw json.RawMessage) (string, bool) {
	var content markupContent
	if err := json.Unmarshal(raw, &content); err != nil || content.Value == "" {
		return "", false
	}
	return content.Value, true
}

// joinedProse reads the array form, joining its parts with a blank line.
func joinedProse(raw json.RawMessage) string {
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	joined := ""
	for _, part := range parts {
		if text := proseOf(part); text != "" {
			joined = appendParagraph(joined, text)
		}
	}
	return joined
}

// appendParagraph joins two pieces of prose with a blank line between them.
func appendParagraph(joined, next string) string {
	if joined == "" {
		return next
	}
	return joined + "\n\n" + next
}

// signatureOf reads a signature-help answer.
//
// The two indices are CLAMPED. A server's arithmetic error must not become an
// index past the end of a slice in the window that drew it.
func signatureOf(answer *wireSignatureHelp) language.SignatureHelp {
	if answer == nil || len(answer.Signatures) == 0 {
		return language.SignatureHelp{}
	}
	out := language.SignatureHelp{Signatures: signaturesOf(answer.Signatures)}
	out.Active = clamp(answer.ActiveSignature, len(out.Signatures)-1)
	out.Parameter = max(0, answer.ActiveParameter)
	return out
}

// signaturesOf converts the overload list, bounded.
func signaturesOf(list []wireSignature) []language.Signature {
	out := make([]language.Signature, 0, min(len(list), maxSignatures))
	for _, item := range list[:min(len(list), maxSignatures)] {
		out = append(out, language.Signature{
			Label:         language.CleanLine(item.Label),
			Documentation: language.CleanText(proseOf(item.Documentation)),
			Parameters:    parametersOf(item),
		})
	}
	return out
}

// parametersOf names one signature's parameters.
//
// A parameter's label is a string OR a pair of offsets into the signature's own
// label. The offsets are read as their own text where they fit, because a pane
// that showed "[4 9]" would be showing the operator a wire detail.
func parametersOf(item wireSignature) []string {
	out := make([]string, 0, len(item.Parameters))
	for _, p := range item.Parameters {
		out = append(out, language.CleanLine(parameterLabel(item.Label, p.Label)))
	}
	return out
}

// parameterLabel resolves one parameter's label against its signature's.
func parameterLabel(signature string, raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var span [2]int
	if err := json.Unmarshal(raw, &span); err != nil {
		return ""
	}
	if span[0] < 0 || span[1] > len(signature) || span[0] > span[1] {
		return ""
	}
	return signature[span[0]:span[1]]
}

// highlightsOf converts the occurrence list.
func highlightsOf(list []wireHighlight) []language.Highlight {
	out := make([]language.Highlight, 0, min(len(list), maxLocations))
	for _, item := range list[:min(len(list), maxLocations)] {
		out = append(out, language.Highlight{
			Span: rangeOf(item.Range), Kind: highlightKindOf(item.Kind),
		})
	}
	return out
}

// highlightKindOf reads the protocol's number, defaulting to a plain mention.
func highlightKindOf(kind int) language.HighlightKind {
	if kind < int(language.HighlightText) || kind > int(language.HighlightWrite) {
		return language.HighlightText
	}
	return language.HighlightKind(kind)
}

// prepareOf reads a prepareRename answer.
//
// An answer that fits none of the protocol's three shapes is read as "this
// cannot be renamed", which is the safe direction: refusing a rename that would
// have worked costs a keystroke, where allowing one the server cannot do
// produces an edit set nobody can predict.
func prepareOf(answer *wirePrepareRename) language.PrepareRename {
	if answer == nil {
		return language.PrepareRename{}
	}
	out := language.PrepareRename{
		Allowed:     true,
		Placeholder: language.CleanLine(answer.Placeholder),
		Span:        rangeOf(prepareSpan(answer)),
	}
	return out
}

// prepareSpan is the span a prepareRename answer carries, in either of the two
// shapes that carry one.
func prepareSpan(answer *wirePrepareRename) wireRange {
	if answer.Range != nil {
		return *answer.Range
	}
	return wireRange{Start: answer.Start, End: answer.End}
}

// editsOf converts one file's edits, bounded.
func editsOf(list []wireTextEdit) []language.TextEdit {
	out := make([]language.TextEdit, 0, min(len(list), maxEdits))
	for _, item := range list[:min(len(list), maxEdits)] {
		out = append(out, language.TextEdit{Span: rangeOf(item.Range), NewText: item.NewText})
	}
	return out
}

// workspaceEditOf reads both shapes a cross-file change takes.
//
// `documentChanges` is preferred where it is present: it is ordered, where
// `changes` is a map whose iteration order is the runtime's — and a preview
// that listed the same rename's files in a different order each time is one
// nobody trusts.
func workspaceEditOf(answer *wireWorkspaceEdit) language.WorkspaceEdit {
	if answer == nil {
		return language.WorkspaceEdit{}
	}
	if len(answer.DocumentChanges) > 0 {
		return language.WorkspaceEdit{Files: documentChangesOf(answer.DocumentChanges)}
	}
	return language.WorkspaceEdit{Files: fileEditsOf(answer.Changes)}
}

// documentChangesOf reads the ordered form.
//
// A file OPERATION — a create, a rename or a delete — is skipped rather than
// applied. This editor changes text; a server that wants a file moved is asking
// for something the operator did not agree to when they typed a new name.
func documentChangesOf(raw []json.RawMessage) []language.FileEdits {
	out := make([]language.FileEdits, 0, min(len(raw), maxEditFiles))
	for _, entry := range raw[:min(len(raw), maxEditFiles)] {
		var change wireDocumentEdit
		if err := json.Unmarshal(entry, &change); err != nil {
			continue
		}
		if change.Kind != "" || change.TextDocument == nil {
			continue
		}
		if path, err := uriToPath(change.TextDocument.URI); err == nil {
			out = append(out, language.FileEdits{Path: path, Edits: editsOf(change.Edits)})
		}
	}
	return out
}

// fileEditsOf reads the map form, in a STABLE order.
//
// Sorted by path, because a map's iteration order is the runtime's and a
// preview of a rename must list the same files in the same order every time it
// is shown.
func fileEditsOf(changes map[string][]wireTextEdit) []language.FileEdits {
	out := make([]language.FileEdits, 0, min(len(changes), maxEditFiles))
	for _, uri := range sortedKeys(changes) {
		path, err := uriToPath(uri)
		if err != nil {
			continue
		}
		out = append(out, language.FileEdits{Path: path, Edits: editsOf(changes[uri])})
		if len(out) >= maxEditFiles {
			break
		}
	}
	return out
}

// actionsOf reads the code-action list.
//
// An action whose work is a server-side COMMAND rather than an edit is left
// out. This client declares it does not apply server-initiated edits, so
// running the command would produce a `workspace/applyEdit` this client
// refuses — an entry that appears to work and does nothing, which is the
// failure this project records under three other names.
//
// An action carrying a TOKEN and no edit is kept, because it is the opposite
// case: the server has not refused to say what it would do, it is waiting to be
// asked (`codeAction/resolve`). Every refactoring gopls offers arrives this way
// — and dropping them was an editor that could show "Extract function" and
// never carry it out.
func actionsOf(raw []json.RawMessage) []language.CodeAction {
	out := make([]language.CodeAction, 0, min(len(raw), maxActions))
	for _, entry := range raw[:min(len(raw), maxActions)] {
		var action wireCodeAction
		if err := json.Unmarshal(entry, &action); err != nil {
			continue
		}
		if !runnableAction(action) {
			continue
		}
		out = append(out, language.CodeAction{
			Title: language.CleanLine(action.Title), Kind: language.CleanLine(action.Kind),
			Preferred: action.IsPreferred, Edit: workspaceEditOf(action.Edit),
			Data: dataOf(action.Data),
		})
	}
	return out
}

// runnableAction reports whether an offer is one this editor can carry out: it
// has a name, it is not marked unavailable, and it either carries its change or
// can be asked for it.
func runnableAction(action wireCodeAction) bool {
	if action.Title == "" || action.Disabled != nil {
		return false
	}
	return action.Edit != nil || len(action.Data) > 0
}

// dataOf keeps a server's opaque token, copied out of the buffer the decoder
// owns — a RawMessage points into the response, and the answer outlives it.
func dataOf(raw json.RawMessage) []byte {
	if len(raw) == 0 || len(raw) > maxActionData {
		return nil
	}
	return append([]byte(nil), raw...)
}

// spanOf converts a range onto the wire by value.
//
// `wireRangeOf` answers a POINTER, because a document change's range is
// optional and its absence means "the whole document". A request that names a
// selection always has one, so it takes the value form rather than
// dereferencing a pointer that can never be nil.
func spanOf(r language.Range) wireRange {
	return wireRange{Start: wirePositionOf(r.Start), End: wirePositionOf(r.End)}
}

// wirePositionOf converts a position onto the wire.
func wirePositionOf(p language.Position) wirePosition {
	return wirePosition{Line: max(0, p.Line), Character: max(0, p.Character)}
}

// wireDiagnosticsOf puts the problems a code action should address back on the
// wire, in the shape they arrived in.
func wireDiagnosticsOf(list []language.Diagnostic) []wireDiagnostic {
	out := make([]wireDiagnostic, 0, len(list))
	for _, d := range list {
		out = append(out, wireDiagnostic{
			Range:    spanOf(d.Range),
			Severity: int(d.Severity), Source: d.Source, Message: d.Message,
		})
	}
	return out
}

// formatOptionsOf converts the editor's settings onto the wire, refusing a tab
// stop of zero — which no editor has and which some formatters divide by.
func formatOptionsOf(o language.FormatOptions) wireFormatOptions {
	return wireFormatOptions{TabSize: max(1, o.TabSize), InsertSpaces: o.InsertSpaces}
}

// clamp holds an index inside a list.
func clamp(i, last int) int { return max(0, min(i, last)) }
