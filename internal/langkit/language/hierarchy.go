package language

// The two hierarchies: who calls this, and what is above and below this type.
//
// Both are asked in TWO steps, which is the protocol's shape and not an
// accident of it: `prepare` turns a position into an ITEM the server will
// answer about, and every question after that is asked about the item rather
// than about a place. That is what makes a tree expandable — a node three
// levels down is a file and a line this window has never opened, and asking
// "what calls this" about a position would mean opening it first.
//
// Nothing here walks anything. The server answers one level at a time and the
// window asks for the next when the operator opens a row.

// HierarchyItem is one node: a function in a call tree, a type in a type tree.
type HierarchyItem struct {
	// Name is what the row letters, and Detail what sits beside it — a
	// package, a receiver, a signature. Both are a subprocess's prose and have
	// been scrubbed.
	Name   string
	Detail string

	// Kind is the protocol's symbol kind, kept as the number the server sent.
	// It is what tells a function from a method from an interface, and this
	// editor draws it rather than reasoning about it.
	Kind int

	// Path is the file, Span the whole declaration, and Selection the NAME
	// inside it — which is where opening a row lands, because the top of a
	// forty-line function is not where its name is.
	Path      string
	Span      Range
	Selection Range

	// Data is the server's opaque token for this item, handed back verbatim
	// when its children are asked for. It is never read here.
	Data []byte
}

// Zero reports whether an item names nothing, which is what a prepare on a
// keyword or a comment answers with.
func (h HierarchyItem) Zero() bool { return h.Name == "" && h.Path == "" }

// HierarchyEdge is one node with the places it is used from.
//
// The RANGES are what make a call hierarchy more than a list of functions: a
// caller that calls this three times is one row and three places, and an editor
// that threw them away would send the operator to the top of the caller to find
// the call by eye.
type HierarchyEdge struct {
	Item HierarchyItem

	// Ranges are in the CALLER's file for an incoming call and in the caller's
	// file for an outgoing one too — the protocol puts them where the call is
	// written, which is the file being looked at in one direction and the
	// parent's file in the other.
	Ranges []Range
}

// MaxHierarchyRows bounds one answer. A function called from ten thousand
// places is a real thing in a large tree, and a pane holding all of them is one
// nobody scrolls: the rows past this are not drawn, and the window says so.
const MaxHierarchyRows = 500

// Direction is which way a hierarchy is read.
//
// One type for both trees, because the question is the same shape either way —
// up or down — and the two features it maps to are a table row rather than a
// branch (see hierarchyFeatures).
type Direction int

// The two directions, and what they mean in each tree.
const (
	// Up is callers, and supertypes: what this depends ON is below, so what
	// depends on THIS is up.
	Up Direction = iota

	// Down is callees, and subtypes.
	Down
)

// String names a direction for a message the operator reads.
func (d Direction) String() string {
	if d == Down {
		return "down"
	}
	return "up"
}

// Tree is which hierarchy is being read.
type Tree int

// The two trees.
const (
	// Calls is the call hierarchy: who calls this function, and what it calls.
	Calls Tree = iota

	// Types is the type hierarchy: what this type implements, and what
	// implements it.
	Types
)

// Prepare is the feature that turns a position into an item for one tree.
func (t Tree) Prepare() Feature {
	if t == Types {
		return FeaturePrepareTypeHierarchy
	}
	return FeaturePrepareCallHierarchy
}

// Step is the feature that asks for one level in a direction.
//
// A TABLE rather than four branches spread through the window (Rule 6): the
// two trees and the two directions are four questions, and every caller wants
// the same mapping.
func (t Tree) Step(d Direction) Feature { return hierarchyFeatures[hierarchyStep{tree: t, way: d}] }

// hierarchyStep is one tree read one way.
type hierarchyStep struct {
	tree Tree
	way  Direction
}

// hierarchyFeatures is what each of the four asks for.
var hierarchyFeatures = map[hierarchyStep]Feature{
	{tree: Calls, way: Up}:   FeatureIncomingCalls,
	{tree: Calls, way: Down}: FeatureOutgoingCalls,
	{tree: Types, way: Up}:   FeatureSupertypes,
	{tree: Types, way: Down}: FeatureSubtypes,
}
