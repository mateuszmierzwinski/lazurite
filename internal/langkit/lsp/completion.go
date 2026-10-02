package lsp

import (
	"context"
	"encoding/json"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// The completion request, its answer's three shapes, and the resolve that
// fills in one row.
//
// Kept in its own file rather than beside the other twelve questions because it
// is the one question asked WHILE THE OPERATOR TYPES: it is asked far more
// often than all the others together, its answer is far larger, and its rules
// about what may be believed are stricter for both reasons.

// The two methods. Resolve is `completionItem/resolve`, not a textDocument
// request: it is about an item, not a place.
const (
	methodCompletion        = "textDocument/completion"
	methodCompletionResolve = "completionItem/resolve"
)

// How a completion list was asked for, in the protocol's numbers.
const (
	// triggerTyped is the operator typing an identifier, or asking for the
	// list themselves.
	triggerTyped = 1

	// triggerCharacter is one of the characters the server named — a dot, a
	// quote. Servers rank differently for it, which is why it is sent.
	triggerCharacter = 2
)

// insertFormatSnippet is the protocol's number for insert text that carries tab
// stops. Plain text is 1.
const insertFormatSnippet = 2

// completionParams is a place, plus how the list was asked for.
type completionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     wirePosition           `json:"position"`
	Context      completionContext      `json:"context"`
}

// completionContext says what opened the list.
type completionContext struct {
	TriggerKind      int    `json:"triggerKind"`
	TriggerCharacter string `json:"triggerCharacter,omitempty"`
}

// wireCompletionList is the OBJECT form of the answer: a list that may be
// incomplete. A server may also answer with a bare array or with null, which is
// why the raw message is examined before this is decoded into.
type wireCompletionList struct {
	IsIncomplete bool                 `json:"isIncomplete"`
	Items        []wireCompletionItem `json:"items"`
}

// wireCompletionItem is one offer.
//
// TextEdit is raw because it has two forms — a plain `TextEdit` and an
// `InsertReplaceEdit` carrying two ranges — and a client that decoded only the
// first silently inserts at the wrong span against a server that sends the
// second.
type wireCompletionItem struct {
	Label               string            `json:"label"`
	Kind                int               `json:"kind"`
	Detail              string            `json:"detail"`
	Documentation       json.RawMessage   `json:"documentation"`
	SortText            string            `json:"sortText"`
	FilterText          string            `json:"filterText"`
	InsertText          string            `json:"insertText"`
	InsertTextFormat    int               `json:"insertTextFormat"`
	TextEdit            json.RawMessage   `json:"textEdit"`
	AdditionalTextEdits []wireTextEdit    `json:"additionalTextEdits"`
	Deprecated          bool              `json:"deprecated"`
	Preselect           bool              `json:"preselect"`
	Tags                []int             `json:"tags"`
	Data                json.RawMessage   `json:"data"`
	LabelDetails        *wireLabelDetails `json:"labelDetails"`
	Command             json.RawMessage   `json:"command"`
}

// wireLabelDetails is the newer place a server puts the signature that used to
// go in `detail`. gopls sends it, and an editor reading only `detail` shows a
// column of bare names.
type wireLabelDetails struct {
	Detail      string `json:"detail"`
	Description string `json:"description"`
}

// wireInsertReplaceEdit is the two-range form of an item's edit.
type wireInsertReplaceEdit struct {
	NewText string     `json:"newText"`
	Insert  *wireRange `json:"insert"`
	Replace *wireRange `json:"replace"`
}

// tagDeprecated is the protocol's tag for an item not to be used. The boolean
// field it replaced is still sent by older servers, so both are read.
const tagDeprecated = 1

// suggestions answers "what may I type here".
type suggestions struct{}

// Handle asks for the list.
func (suggestions) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	params := completionParams{
		TextDocument: textDocumentIdentifier{URI: pathToURI(q.Path)},
		Position:     wirePositionOf(q.Position),
		Context:      askedBy(q.Trigger),
	}
	var raw json.RawMessage
	if err := client.Request(ctx, methodCompletion, params, &raw); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Completions: completionsOf(raw)}
}

// askedBy says how the list was asked for, in the protocol's terms.
func askedBy(trigger string) completionContext {
	if trigger == "" {
		return completionContext{TriggerKind: triggerTyped}
	}
	return completionContext{TriggerKind: triggerCharacter, TriggerCharacter: trigger}
}

// resolving fills in the one item the operator is looking at.
type resolving struct{}

// Handle hands the server back its own item and reads what it adds.
//
// An item that comes back MALFORMED keeps what it already had rather than
// becoming an empty row: resolve adds documentation and edits, and a failed
// resolve must not take away the label the operator is reading.
func (resolving) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var raw json.RawMessage
	if err := client.Request(ctx, methodCompletionResolve, itemToWire(q.Item), &raw); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Item: resolvedFrom(q.Item, raw)}
}
