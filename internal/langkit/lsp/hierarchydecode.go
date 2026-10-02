package lsp

import (
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// Reading a hierarchy's answers.
//
// Both trees answer with the same node under different field names, so there is
// one item decoder and two thin shapes over it. A node whose URI this client
// cannot read is DROPPED rather than kept with an empty path: a row that opens
// nothing is worse than a row that is not there.

// hierarchyItemsOf reads a list of nodes.
func hierarchyItemsOf(list []wireHierarchyItem) []language.HierarchyItem {
	out := make([]language.HierarchyItem, 0, min(len(list), language.MaxHierarchyRows))
	for _, item := range list[:min(len(list), language.MaxHierarchyRows)] {
		if one, ok := hierarchyItemOf(item); ok {
			out = append(out, one)
		}
	}
	return out
}

// hierarchyItemOf reads one node, false where it names no file this editor can
// open.
func hierarchyItemOf(item wireHierarchyItem) (language.HierarchyItem, bool) {
	path, err := uriToPath(item.URI)
	if err != nil || item.Name == "" {
		return language.HierarchyItem{}, false
	}
	return language.HierarchyItem{
		Name:   language.CleanLine(item.Name),
		Detail: language.CleanLine(item.Detail),
		Kind:   item.Kind,
		Path:   path,
		Span:   rangeOf(item.Range),
		// A server that sent no selection range gets the declaration's own,
		// which is where the jump lands then. An empty one would send every
		// row to the top of its file.
		Selection: selectionOrSpan(item),
		Data:      dataOf(item.Data),
	}, true
}

// selectionOrSpan is the span a row opens at: the NAME where the server said
// where it was, and the declaration otherwise.
func selectionOrSpan(item wireHierarchyItem) language.Range {
	selection := rangeOf(item.SelectionRange)
	if selection == (language.Range{}) {
		return rangeOf(item.Range)
	}
	return selection
}

// callEdgesOf reads one level of a call hierarchy.
//
// `from` and `to` are the same field in opposite directions and exactly one is
// ever set, so the one that is present is the node.
func callEdgesOf(list []wireCall) []language.HierarchyEdge {
	out := make([]language.HierarchyEdge, 0, min(len(list), language.MaxHierarchyRows))
	for _, call := range list[:min(len(list), language.MaxHierarchyRows)] {
		node, ok := nodeOfCall(call)
		if !ok {
			continue
		}
		out = append(out, language.HierarchyEdge{Item: node, Ranges: rangesOf(call.FromRanges)})
	}
	return out
}

// nodeOfCall is whichever end of the call the server filled in.
func nodeOfCall(call wireCall) (language.HierarchyItem, bool) {
	if call.From != nil {
		return hierarchyItemOf(*call.From)
	}
	if call.To != nil {
		return hierarchyItemOf(*call.To)
	}
	return language.HierarchyItem{}, false
}

// edgesOfItems carries bare nodes into the same shape, for a type hierarchy —
// where a node is not called from anywhere and has no ranges.
func edgesOfItems(items []language.HierarchyItem) []language.HierarchyEdge {
	out := make([]language.HierarchyEdge, 0, len(items))
	for _, item := range items {
		out = append(out, language.HierarchyEdge{Item: item})
	}
	return out
}

// rangesOf converts the places a node is used from.
func rangesOf(list []wireRange) []language.Range {
	out := make([]language.Range, 0, min(len(list), language.MaxHierarchyRows))
	for _, span := range list[:min(len(list), language.MaxHierarchyRows)] {
		out = append(out, rangeOf(span))
	}
	return out
}
