package language

import "strings"

// What a server offers at the caret, in this editor's terms.
//
// The protocol's completion item carries twenty fields, most of them about
// rendering decisions this editor has already made. What is kept here is what
// the popup DRAWS (a label, what kind of thing it is, a one-line detail and the
// documentation beside the selection), what it INSERTS (the text, the span it
// replaces, and the imports that have to go in with it), and what it ORDERS by
// (the server's own sort and filter strings, which are not the label and must
// not be guessed from it — gopls sorts by relevance and encodes that here).

// The bounds a completion list is read under.
const (
	// MaxCompletions bounds one list. A thousand items is a package dot in a
	// large module and is real; more than this is a server fault, and nobody
	// reads past the first screen either way.
	MaxCompletions = 2000

	// MaxCompletionEdits bounds the additional edits one item may carry. An
	// item that adds an import makes one or two; an item that rewrote a file
	// would be doing something else under the name of a completion.
	MaxCompletionEdits = 64
)

// CompletionKind is what sort of thing an item is. The values are the
// protocol's, so a server's number is kept rather than translated.
type CompletionKind int

// The kinds a server may send. The list is the protocol's, in its order.
const (
	KindText CompletionKind = iota + 1
	KindMethod
	KindFunction
	KindConstructor
	KindField
	KindVariable
	KindClass
	KindInterface
	KindModule
	KindProperty
	KindUnit
	KindValue
	KindEnum
	KindKeyword
	KindSnippet
	KindColor
	KindFile
	KindReference
	KindFolder
	KindEnumMember
	KindConstant
	KindStruct
	KindEvent
	KindOperator
	KindTypeParameter
)

// kindNames is what each kind is called in the list the operator reads.
//
// A table built once at package scope (Rule 6.1, 6.2), and in the operator's
// words rather than the protocol's: "field" and "package" are what a Go
// programmer calls these, and `KindModule` is what gopls sends for a package.
var kindNames = map[CompletionKind]string{
	KindText: "text", KindMethod: "method", KindFunction: "func",
	KindConstructor: "new", KindField: "field", KindVariable: "var",
	KindClass: "type", KindInterface: "interface", KindModule: "package",
	KindProperty: "property", KindUnit: "unit", KindValue: "value",
	KindEnum: "enum", KindKeyword: "keyword", KindSnippet: "snippet",
	KindColor: "colour", KindFile: "file", KindReference: "ref",
	KindFolder: "folder", KindEnumMember: "enum member", KindConstant: "const",
	KindStruct: "struct", KindEvent: "event", KindOperator: "operator",
	KindTypeParameter: "type param",
}

// String names a kind for the row it is drawn on, and is EMPTY for one this
// editor has no word for — a blank column says "no idea" honestly, where
// "unknown" would look like a kind of its own.
func (k CompletionKind) String() string { return kindNames[k] }

// Completion is one thing the server offers.
type Completion struct {
	// Label is what the row says, and what is filtered against unless the
	// server named a Filter of its own.
	Label string

	// Kind is what sort of thing it is, for the row's second column.
	Kind CompletionKind

	// Detail is the one-line signature or type beside the label.
	Detail string

	// Documentation is the prose shown beside the SELECTED row. It often
	// arrives only after Resolve, which is the whole reason resolve exists: a
	// server that sent the documentation of every item would send a megabyte
	// to draw twelve rows.
	Documentation string

	// Sort is what the list is ordered by and Filter what it is matched
	// against, each falling back to the label where the server named neither.
	// gopls encodes its relevance ranking in Sort, so ordering by the label
	// instead throws away the one thing that makes its list useful.
	Sort   string
	Filter string

	// Insert is the text that goes in, which is NOT always the label: a
	// function may complete as `Open(` and a package as its import path.
	Insert string

	// Snippet says Insert is a template with tab stops rather than plain text.
	// Inserting one as-is would put `${1:name}` in the operator's file, so a
	// consumer that cannot place tab stops must not use it — which is why the
	// client does not ASK for snippets until something can.
	Snippet bool

	// Replace is the span Insert replaces, where the server named one. Without
	// it a consumer has to guess the word under the caret, and the guess is
	// wrong wherever the server would have replaced more — a qualified name,
	// an interpolation.
	Replace    Range
	HasReplace bool

	// Additional are edits elsewhere in the file that go in WITH the item:
	// gopls adds the import a completed package needs. They are part of the
	// same insertion and are applied in the same step or not at all.
	Additional []TextEdit

	// Deprecated marks an item the server says not to use, drawn struck
	// through rather than hidden: it is still what the operator asked for.
	Deprecated bool

	// Preselect is the server's own suggestion of which row to start on.
	Preselect bool

	// Data is the server's opaque token for this item, handed back verbatim on
	// resolve. It is never read here: it belongs to the server.
	Data []byte

	// Resolved says the documentation and edits have already been asked for,
	// so a second resolve of the same row is not sent.
	Resolved bool
}

// SortKey is what the list is ordered by, and FilterKey what it is matched
// against. Both fall back to the label rather than to the empty string, or an
// item the server said nothing about would sort before everything.
func (c Completion) SortKey() string { return orLabel(c.Sort, c.Label) }

// FilterKey is what a typed prefix is matched against.
func (c Completion) FilterKey() string { return orLabel(c.Filter, c.Label) }

// orLabel is the server's string, or the label where it named none.
func orLabel(given, label string) string {
	if strings.TrimSpace(given) == "" {
		return label
	}
	return given
}

// CompletionList is what a server offered at one point in one file.
type CompletionList struct {
	Items []Completion

	// Incomplete says the list depends on what is typed next, so the next
	// keystroke must ASK AGAIN rather than filter what is here. gopls sets it
	// constantly, and a client that filtered an incomplete list locally shows
	// a shrinking list of the wrong candidates.
	Incomplete bool
}
