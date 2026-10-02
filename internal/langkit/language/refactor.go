package language

// Refactorings asked for BY NAME.
//
// A code action is "what can be done here", and its answer is a menu to read. A
// refactoring is "extract this into a function", and its answer is that change
// or nothing at all — which is what makes it a keystroke rather than a hunt
// through Ctrl+.
//
// Nothing here TRANSFORMS anything. The server does the work; this is the
// vocabulary for asking it for one particular piece of it. There is no Go
// refactoring engine in this editor and there is not going to be one: what a
// server cannot do is reported as refused, not half-built here.

// Refactoring is one named change this editor can ask a server for.
type Refactoring int

// The named changes.
const (
	// ExtractFunction lifts a selection into a function of its own — or into a
	// METHOD where it came from inside one, which is the same request with a
	// receiver and is offered under the same name.
	ExtractFunction Refactoring = iota

	// ExtractVariable gives an expression a name.
	ExtractVariable

	// ExtractConstant is the same for an expression that is constant, which
	// servers answer as a kind of its own rather than as a variable.
	ExtractConstant

	// Inline is the opposite of extracting: a call replaced by what it does, or
	// a variable by what it holds.
	Inline

	// ChangeSignature is what a server can do to a parameter list. It is the
	// one named change here that is NOT the whole of what the same command does
	// in an IDE with its own refactoring engine — see refactorings.
	ChangeSignature
)

// AllRefactorings is every named change, in the order they are offered.
//
// Exported so the window can be held to it: a refactoring named here and
// reachable from nothing is the "a feature nobody can find" failure, and a test
// walks this list to say so.
var AllRefactorings = []Refactoring{
	ExtractFunction, ExtractVariable, ExtractConstant, Inline, ChangeSignature,
}

// refactoring is what one named change is called, what it asks a server for,
// and whether it is about code the operator chose.
type refactoring struct {
	name      string
	kinds     []string
	selection bool
}

// refactorings is the whole vocabulary, built once at package scope (Rule 6.1).
//
// The kinds are the protocol's own, and they are LEAVES rather than the family
// they belong to. Kinds are hierarchical — "refactor" subsumes
// "refactor.extract", which subsumes "refactor.extract.function" — and a server
// narrows its answer to what was asked for, so asking for the family would
// answer Extract Variable with an extracted function and a menu to choose from.
// Naming the leaves is what makes each of these one action with one outcome.
//
// The leaves are spelled the way gopls spells them, which is the way the
// protocol's own examples spell them. A server that publishes only family kinds
// answers these requests with nothing, and the window says so plainly rather
// than guessing at which of its offers was meant.
//
// CHANGE SIGNATURE is the honest exception. gopls has no change-signature
// dialog: what it has is three rewrites of a parameter list — drop the one
// nothing uses, move one left, move one right. Those are what this asks for.
// Renaming a parameter is Rename Symbol; changing a type or adding a parameter
// is not offered, because no server here can do it.
var refactorings = map[Refactoring]refactoring{
	ExtractFunction: {
		name:      "extract function",
		kinds:     []string{"refactor.extract.function", "refactor.extract.method"},
		selection: true,
	},
	ExtractVariable: {
		name:      "extract variable",
		kinds:     []string{"refactor.extract.variable", "refactor.extract.variable-all"},
		selection: true,
	},
	ExtractConstant: {
		name:      "extract constant",
		kinds:     []string{"refactor.extract.constant", "refactor.extract.constant-all"},
		selection: true,
	},
	Inline: {
		name:  "inline",
		kinds: []string{"refactor.inline.call", "refactor.inline.variable"},
	},
	ChangeSignature: {
		name: "change signature",
		kinds: []string{
			"refactor.rewrite.removeUnusedParam",
			"refactor.rewrite.moveParamLeft",
			"refactor.rewrite.moveParamRight",
		},
	},
}

// String names a refactoring in a message the operator reads.
func (r Refactoring) String() string { return refactorings[r].name }

// Kinds are what this refactoring asks a server for, for a code action query's
// Only field. Empty for a value that names no refactoring, which asks for
// everything — so a caller checks Known first.
func (r Refactoring) Kinds() []string { return refactorings[r].kinds }

// NeedsSelection reports whether the change is about code the operator chose.
//
// Extraction is: a server refuses an empty range, and an operator who pressed
// the key with nothing selected would be told the server offers nothing here —
// which is true and useless. Inlining is not: it is about the call the caret is
// in.
func (r Refactoring) NeedsSelection() bool { return refactorings[r].selection }

// Known reports whether a value names a refactoring this editor asks for.
func (r Refactoring) Known() bool {
	_, known := refactorings[r]
	return known
}
