package lsp

import (
	"context"
	"encoding/json"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// The two hierarchies, six requests.
//
// Both are asked in TWO steps — a prepare that turns a position into an item,
// then a question about that item — and both answer with the same shape of node
// under different field names. That is what this file is: six thin handlers
// over two decoders, rather than six decoders.

// The methods. The steps are `callHierarchy/` and `typeHierarchy/` requests
// rather than `textDocument/` ones, because they are about an ITEM and not
// about a place.
const (
	methodPrepareCallHierarchy = "textDocument/prepareCallHierarchy"
	methodIncomingCalls        = "callHierarchy/incomingCalls"
	methodOutgoingCalls        = "callHierarchy/outgoingCalls"
	methodPrepareTypeHierarchy = "textDocument/prepareTypeHierarchy"
	methodSupertypes           = "typeHierarchy/supertypes"
	methodSubtypes             = "typeHierarchy/subtypes"
)

// wireHierarchyItem is one node of either tree. The two protocol types differ
// only in which fields are optional, so one shape reads both.
type wireHierarchyItem struct {
	Name           string          `json:"name"`
	Kind           int             `json:"kind"`
	Detail         string          `json:"detail"`
	URI            string          `json:"uri"`
	Range          wireRange       `json:"range"`
	SelectionRange wireRange       `json:"selectionRange"`
	Data           json.RawMessage `json:"data"`
}

// wireCall is one edge of a call hierarchy. `from` and `to` are the same field
// in opposite directions, and exactly one of them is ever set.
type wireCall struct {
	From       *wireHierarchyItem `json:"from"`
	To         *wireHierarchyItem `json:"to"`
	FromRanges []wireRange        `json:"fromRanges"`
}

// hierarchyItemParams is what a step asks about: the item, as it arrived.
type hierarchyItemParams struct {
	Item wireHierarchyItem `json:"item"`
}

// preparing turns a position into the item a hierarchy is read from.
//
// A named struct per METHOD in the registry, differing in one field (Rule 5.1):
// the prepares are the same request with different names, as the three "where
// is it" questions already are.
type preparing struct{ method string }

// Handle asks for the item under the caret.
func (p preparing) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var answer []wireHierarchyItem
	if err := client.Request(ctx, p.method, at(q), &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Roots: hierarchyItemsOf(answer)}
}

// calling asks for one level of a CALL hierarchy.
type calling struct{ method string }

// Handle asks for the callers or the callees of an item.
func (c calling) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var answer []wireCall
	if err := client.Request(ctx, c.method, hierarchyParamsOf(q), &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Edges: callEdgesOf(answer)}
}

// typing asks for one level of a TYPE hierarchy, which answers with bare items
// rather than with edges: a supertype is not called from anywhere.
type typing struct{ method string }

// Handle asks for the types above or below an item.
func (t typing) Handle(ctx context.Context, client language.Client, q language.Query) language.Answer {
	var answer []wireHierarchyItem
	if err := client.Request(ctx, t.method, hierarchyParamsOf(q), &answer); err != nil {
		return language.Answer{Err: err}
	}
	return language.Answer{Edges: edgesOfItems(hierarchyItemsOf(answer))}
}

// hierarchyParamsOf puts the item back on the wire as the server sent it.
func hierarchyParamsOf(q language.Query) hierarchyItemParams {
	return hierarchyItemParams{Item: itemToWireHierarchy(q.Node)}
}

// itemToWireHierarchy is one node on its way back to the server.
//
// The TOKEN is what matters and the rest is what the protocol requires beside
// it: a server is entitled to read the item it is handed rather than its own
// `data`, and one that found a different name or range there would answer about
// something else.
func itemToWireHierarchy(item language.HierarchyItem) wireHierarchyItem {
	return wireHierarchyItem{
		Name: item.Name, Kind: item.Kind, Detail: item.Detail,
		URI:            pathToURI(item.Path),
		Range:          spanOf(item.Span),
		SelectionRange: spanOf(item.Selection),
		Data:           json.RawMessage(item.Data),
	}
}
