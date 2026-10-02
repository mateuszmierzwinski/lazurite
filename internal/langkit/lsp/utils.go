package lsp

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// uriToPath is the reverse, and it REFUSES anything that is not a local file.
//
// A URI in a server's answer is input, not a fact: a go-to-definition that
// followed `http://` or a remote host would take the editor somewhere the
// operator never asked to go. Confining the result to the workspace is a second
// check and a separate decision — `language.Confined`.
func uriToPath(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", ErrBadURI
	}
	if parsed.Scheme != "file" {
		return "", ErrBadURI
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		return "", ErrBadURI
	}
	if parsed.Path == "" {
		return "", ErrBadURI
	}
	return filepath.FromSlash(parsed.Path), nil
}

// provided says whether a `boolean | object` capability was offered.
//
// Present and neither `false` nor `null`. Decoding these as bool works until
// the first server that answers with an options object, at which point every
// feature it configures silently disappears — which reads as the server not
// supporting hover rather than as this client failing to parse it.
func provided(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	switch trimmed {
	case "", "false", "null":
		return false
	}
	return true
}

// syncOf reconciles the two forms textDocumentSync takes.
//
// A number is the sync kind alone, and the protocol's own default for the rest:
// open/close notifications are expected and saves are not sent. An object says
// each of them explicitly.
func syncOf(raw json.RawMessage) syncSupport {
	if kind, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
		return syncSupport{Kind: kind, OpenClose: kind != int(language.SyncNone)}
	}
	var options syncOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return syncSupport{Kind: int(language.SyncFull), OpenClose: true}
	}
	return syncSupport{
		Kind: options.Change, OpenClose: options.OpenClose,
		Save: provided(options.Save), IncludeText: includesText(options.Save),
	}
}

// includesText says whether the server asked to be sent a saved file's text.
func includesText(raw json.RawMessage) bool {
	var options saveOptions
	return json.Unmarshal(raw, &options) == nil && options.IncludeText
}

// prepareRenameOf says whether renameProvider offered the prepare step.
func prepareRenameOf(raw json.RawMessage) bool {
	var options renameOptions
	return json.Unmarshal(raw, &options) == nil && options.Prepare
}

// capabilitiesOf translates the server's capability object into the editor's
// vocabulary.
//
// A capability object that cannot be decoded yields the ZERO value, which
// supports nothing: a server whose answer this client cannot read is a server
// whose features must not be offered.
func capabilitiesOf(raw json.RawMessage) language.Capabilities {
	var wire serverCapabilities
	if err := json.Unmarshal(raw, &wire); err != nil {
		return language.Capabilities{}
	}
	sync := syncOf(wire.TextDocumentSync)
	caps := queryCapabilities(wire)
	caps.Sync = language.SyncKind(sync.Kind)
	caps.OpenClose, caps.Save, caps.SaveIncludesText = sync.OpenClose, sync.Save, sync.IncludeText
	caps.CompletionTrigger = triggersOf(wire.Completion)
	caps.SignatureTrigger = triggersOf(wire.SignatureHelp)
	caps.Raw = append([]byte{}, raw...)
	return caps
}

// queryCapabilities is the flat half of the translation: the providers that are
// nothing but present or absent.
//
// Split from capabilitiesOf because the two halves are read for different
// reasons — one is a list to check against, the other is negotiation — and
// together they are a body no one reads to the end.
func queryCapabilities(wire serverCapabilities) language.Capabilities {
	return language.Capabilities{
		Hover: provided(wire.Hover), Definition: provided(wire.Definition),
		TypeDefinition:    provided(wire.TypeDefinition),
		Implementation:    provided(wire.Implementation),
		References:        provided(wire.References),
		DocumentHighlight: provided(wire.DocumentHighlight),
		DocumentSymbol:    provided(wire.DocumentSymbol),
		WorkspaceSymbol:   provided(wire.WorkspaceSymbol),
		Completion:        wire.Completion != nil,
		CompletionResolve: wire.Completion != nil && wire.Completion.ResolveProvider,
		SignatureHelp:     wire.SignatureHelp != nil,
		CodeAction:        provided(wire.CodeAction),
		CodeActionResolve: resolvesActions(wire.CodeAction),
		CodeLens:          provided(wire.CodeLens),
		InlayHint:         provided(wire.InlayHint), SemanticTokens: provided(wire.SemanticTokens),
		CallHierarchy:   provided(wire.CallHierarchy),
		TypeHierarchy:   provided(wire.TypeHierarchy),
		Formatting:      provided(wire.Formatting),
		RangeFormatting: provided(wire.RangeFormatting),
		Rename:          provided(wire.Rename),
		PrepareRename:   prepareRenameOf(wire.Rename),
	}
}

// resolvesActions reports whether a server will fill in an action's edit when
// asked.
//
// The provider takes two forms — `true` and an options object — and only the
// object can say so. A bare `true` is a server that answers with the whole
// action at once, which is a server this editor can also work with; what it is
// not is a server to send `codeAction/resolve` to.
func resolvesActions(raw json.RawMessage) bool {
	var options codeActionOptions
	if json.Unmarshal(raw, &options) != nil {
		return false
	}
	return options.ResolveProvider
}

// triggersOf is a provider's trigger characters, or none.
func triggersOf(provider *triggerProvider) []string {
	if provider == nil {
		return nil
	}
	return provider.TriggerCharacters
}

// changesOf puts the editor's changes into the protocol's shape.
func changesOf(changes []language.Change) []contentChange {
	out := make([]contentChange, 0, len(changes))
	for _, change := range changes {
		out = append(out, contentChange{Range: wireRangeOf(change.Range), Text: change.Text})
	}
	return out
}

// wireRangeOf is a core range on the wire, and nil for a whole-document change.
func wireRangeOf(span *language.Range) *wireRange {
	if span == nil {
		return nil
	}
	return &wireRange{
		Start: wirePosition{Line: span.Start.Line, Character: span.Start.Character},
		End:   wirePosition{Line: span.End.Line, Character: span.End.Character},
	}
}

// openParamsOf is a document being announced.
func openParamsOf(doc language.Document) didOpenParams {
	return didOpenParams{TextDocument: textDocumentItem{
		URI: pathToURI(doc.Path), LanguageID: languageIDOf(doc),
		Version: doc.Version, Text: doc.Text,
	}}
}

// changeParamsOf is a set of edits on the wire.
func changeParamsOf(path string, version int, changes []language.Change) didChangeParams {
	return didChangeParams{
		TextDocument: versionedIdentifier{URI: pathToURI(path), Version: version},
		Changes:      changesOf(changes),
	}
}

// saveParamsOf is a save, with the text only where the server asked for it.
func saveParamsOf(path, text string, includeText bool) didSaveParams {
	params := didSaveParams{TextDocument: textDocumentIdentifier{URI: pathToURI(path)}}
	if includeText {
		params.Text = text
	}
	return params
}

// languageIDOf is the document's own language id, or one worked out from its
// extension.
//
// The caller's answer wins: a file with no extension, or one the table does not
// know, may still be a language the editor has identified some other way — and
// T5.4's "language mode override per file" is exactly that.
func languageIDOf(doc language.Document) string {
	if doc.LanguageID != "" {
		return doc.LanguageID
	}
	return language.LanguageID(doc.Path)
}

// isWholeDocument says whether a change set replaces the entire document, which
// is what a server negotiating full synchronisation requires.
func isWholeDocument(changes []language.Change) bool {
	return len(changes) == 1 && changes[0].Range == nil
}

// decodeResult puts a server's result into the caller's shape.
//
// A nil out is a caller that does not want the answer, and an empty result is
// the protocol's `null` — neither is an error, and treating either as one turns
// a successful shutdown into a failed one.
func decodeResult(raw json.RawMessage, out any) error {
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// backoff is how long to wait before the nth restart attempt.
//
// Doubling from half a second and capped, so a server that is briefly wedged is
// back almost at once while one that cannot start at all is not being launched
// in a tight loop. The cap matters more than the curve: without it the eighth
// attempt is a minute away and the tenth is four.
func backoff(attempt int) time.Duration {
	if attempt <= 1 {
		return baseBackoff
	}
	wait := baseBackoff << min(attempt-1, backoffShiftCap)
	return min(wait, maxBackoff)
}

// The restart backoff's shape.
const (
	baseBackoff = 500 * time.Millisecond
	maxBackoff  = 30 * time.Second
	// backoffShiftCap stops the shift itself overflowing on a large attempt
	// count, which would wrap to a negative duration and restart instantly —
	// the opposite of what a backoff is for.
	backoffShiftCap = 8
)

// launcherOr is the process launcher unless a test supplied its own.
func launcherOr(supplied Launcher) Launcher {
	if supplied != nil {
		return supplied
	}
	return NewLauncher()
}

// lifetimeOr is the window's context, or one that never ends.
//
// A never-ending context is correct only in a test. The composition root always
// supplies the window's, which is what makes a server stop when the window does.
func lifetimeOr(supplied context.Context) context.Context {
	if supplied != nil {
		return supplied
	}
	return context.Background()
}

// durationOr is a configured bound, or the default where none was set.
func durationOr(supplied, fallback time.Duration) time.Duration {
	if supplied > 0 {
		return supplied
	}
	return fallback
}

// restartsOr is the restart budget: unset takes the default, and a negative
// number means never restart, which is what a test wanting one crash asks for.
func restartsOr(supplied int) int {
	if supplied == 0 {
		return defaultMaxRestarts
	}
	return max(supplied, 0)
}

// sortedKeys is a map's keys in order, so a listing built from one is the same
// listing every time it is built.
//
// Go's map iteration is deliberately randomised, and a preview of a rename that
// showed its files in a different order on each viewing is one nobody trusts.
func sortedKeys(m map[string][]wireTextEdit) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// pathToURI is a filesystem path as the protocol spells it.
//
// Through net/url rather than by concatenation, so a path with a space, a hash
// or a percent in it survives: `file:///a/b c#d` is two different documents to
// a server depending on how it parses, and neither is the file meant.
func pathToURI(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	uri := url.URL{Scheme: "file", Path: clean}
	return uri.String()
}
