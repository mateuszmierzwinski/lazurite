package lsp

import (
	"encoding/json"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// Reading a completion answer, under the same three rules as every other
// decoder here: an unfamiliar shape is an EMPTY answer rather than an error,
// every string a person reads is cleaned, and every list is bounded.
//
// The bound matters more here than anywhere else in this package. A list is
// asked for on a keystroke and answered while the operator is still typing, so
// a server that sent a hundred thousand items would cost a frame on every
// character — and the operator reads the first dozen.

// completionsOf reads the three shapes an answer may take: the object form
// with `isIncomplete`, a bare array of items, and null.
func completionsOf(raw json.RawMessage) language.CompletionList {
	if len(raw) == 0 {
		return language.CompletionList{}
	}
	var object wireCompletionList
	if err := json.Unmarshal(raw, &object); err == nil {
		return language.CompletionList{
			Items:      itemsOf(object.Items),
			Incomplete: object.IsIncomplete,
		}
	}
	var list []wireCompletionItem
	if err := json.Unmarshal(raw, &list); err != nil {
		return language.CompletionList{}
	}
	return language.CompletionList{Items: itemsOf(list)}
}

// itemsOf converts a server's items, bounded.
func itemsOf(list []wireCompletionItem) []language.Completion {
	kept := min(len(list), language.MaxCompletions)
	out := make([]language.Completion, 0, kept)
	for _, item := range list[:kept] {
		out = append(out, completionOf(item))
	}
	return out
}

// completionOf reads one item.
func completionOf(item wireCompletionItem) language.Completion {
	out := language.Completion{
		Label:         language.CleanLine(item.Label),
		Kind:          language.CompletionKind(item.Kind),
		Detail:        language.CleanLine(detailOf(item)),
		Documentation: language.CleanText(proseOf(item.Documentation)),
		Sort:          language.CleanLine(item.SortText),
		Filter:        language.CleanLine(item.FilterText),
		Snippet:       item.InsertTextFormat == insertFormatSnippet,
		Additional:    additionalOf(item.AdditionalTextEdits),
		Deprecated:    item.Deprecated || hasTag(item.Tags, tagDeprecated),
		Preselect:     item.Preselect,
		Data:          []byte(item.Data),
	}
	out.Insert, out.Replace, out.HasReplace = insertionOf(item)
	return out
}

// detailOf is the one-line signature beside the label.
//
// `labelDetails.detail` is where a modern server puts it and `detail` is where
// it used to go. gopls sends the newer field, so an editor reading only the
// older one draws a column of bare names beside a column of kinds.
func detailOf(item wireCompletionItem) string {
	if item.LabelDetails != nil && item.LabelDetails.Detail != "" {
		return item.LabelDetails.Detail
	}
	if item.LabelDetails != nil && item.Detail == "" {
		return item.LabelDetails.Description
	}
	return item.Detail
}

// hasTag reports whether an item carries one of the protocol's tags.
func hasTag(tags []int, want int) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

// insertionOf is what an item types in, and the span it replaces.
//
// The precedence is the protocol's: an item's own `textEdit` wins over
// `insertText`, which wins over the label. The edit has two forms and BOTH are
// read — the two-range `InsertReplaceEdit` is what a server sends when it means
// "replace this much if you are overtyping" — and its `replace` range is the
// one taken, because an editor that only ever inserted would leave the tail of
// the word the operator was in the middle of.
//
// The text is NOT cleaned: it goes into the operator's source, where a tab is a
// tab and a newline is a newline. Everything else read here is drawn.
func insertionOf(item wireCompletionItem) (string, language.Range, bool) {
	if text, span, ok := editInsertion(item.TextEdit); ok {
		return text, span, true
	}
	if item.InsertText != "" {
		return item.InsertText, language.Range{}, false
	}
	return item.Label, language.Range{}, false
}

// editInsertion reads an item's textEdit in either of its two forms.
func editInsertion(raw json.RawMessage) (string, language.Range, bool) {
	if len(raw) == 0 {
		return "", language.Range{}, false
	}
	var both wireInsertReplaceEdit
	if err := json.Unmarshal(raw, &both); err != nil {
		return "", language.Range{}, false
	}
	if span := both.Replace; span != nil {
		return both.NewText, rangeOf(*span), true
	}
	if span := both.Insert; span != nil {
		return both.NewText, rangeOf(*span), true
	}
	var plain wireTextEdit
	if err := json.Unmarshal(raw, &plain); err != nil {
		return "", language.Range{}, false
	}
	return plain.NewText, rangeOf(plain.Range), true
}

// additionalOf converts the edits that go in WITH an item — the import gopls
// adds — bounded to what an insertion can honestly be.
func additionalOf(list []wireTextEdit) []language.TextEdit {
	kept := min(len(list), language.MaxCompletionEdits)
	out := make([]language.TextEdit, 0, kept)
	for _, item := range list[:kept] {
		out = append(out, language.TextEdit{Span: rangeOf(item.Range), NewText: item.NewText})
	}
	return out
}

// resolvedFrom is the item the operator is looking at, with whatever the
// server added.
//
// It starts from what was ALREADY THERE and fills in the blanks, rather than
// replacing the item with what came back. A resolve answer is allowed to omit
// everything it is not changing, and a client that swapped the item for it
// would blank the label under the operator's selection.
func resolvedFrom(item language.Completion, raw json.RawMessage) language.Completion {
	item.Resolved = true
	if len(raw) == 0 {
		return item
	}
	var wire wireCompletionItem
	if err := json.Unmarshal(raw, &wire); err != nil {
		return item
	}
	resolved := completionOf(wire)
	resolved.Resolved = true
	return filledFrom(item, resolved)
}

// filledFrom takes what the resolve added and keeps what it left out.
func filledFrom(had, got language.Completion) language.Completion {
	if got.Label == "" {
		got.Label, got.Kind = had.Label, had.Kind
	}
	if got.Detail == "" {
		got.Detail = had.Detail
	}
	if got.Documentation == "" {
		got.Documentation = had.Documentation
	}
	if len(got.Additional) == 0 {
		got.Additional = had.Additional
	}
	if got.Insert == "" || got.Insert == got.Label {
		got.Insert, got.Replace, got.HasReplace = had.Insert, had.Replace, had.HasReplace
		got.Snippet = had.Snippet
	}
	return got
}

// itemToWire is an item as the server first sent it, for handing back on
// resolve.
//
// The server's own `data` goes back UNTOUCHED: it is the server's bookkeeping,
// and resolve is meaningless without it.
func itemToWire(item language.Completion) wireCompletionItem {
	return wireCompletionItem{
		Label: item.Label, Kind: int(item.Kind), Detail: item.Detail,
		SortText: item.Sort, FilterText: item.Filter,
		InsertText: item.Insert, Data: json.RawMessage(item.Data),
	}
}
