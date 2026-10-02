// Package lexer colours code for the editor.
//
// It lives HERE and not in the framework, and that is §1's rule rather than a
// preference: "a toolkit that shipped a highlighter would be choosing the
// language set for every program that draws code, which is the same argument
// that stops Markdown from opening a link". `TextArea.SetRuns` takes the spans;
// where they come from is the application's business.
//
// # What it is, and what it deliberately is not
//
// A SCANNER, not a parser. It reads a line at a time and decides what each run
// of bytes looks like — a comment, a string, a number, a keyword. It builds no
// tree, resolves no identifier and knows nothing about scope, so it cannot tell
// a type from a variable and does not try.
//
// That is the right trade for an editor pane. Colouring exists so the eye can
// skip: a string is not code, a comment is not either, and a keyword is where a
// statement begins. Everything past that costs a real parser per language and
// buys a distinction most readers do not use.
package lexer

import "strings"

// Kind is what a span of source looks like.
type Kind uint8

const (
	// Plain is ordinary code: identifiers, operators, punctuation.
	Plain Kind = iota

	// Comment is a comment, to the end of the line or across lines.
	Comment

	// String is a string or a character literal.
	String

	// Number is a numeric literal.
	Number

	// Keyword is a word the language reserves.
	Keyword

	// Declaration is the name a declaration introduces — `func main`,
	// `type Adapter` — which GoLand sets apart from every later use of it.
	Declaration

	// Heading, Code, Strong, Emphasis and Link are Markdown's: a heading line,
	// inline or fenced code, **strong** and *emphasised* text, and a link.
	Heading
	Code
	Strong
	Emphasis
	Link
)

// Span is one run of a line, and the unit `TextArea.SetRuns` takes.
type Span struct {
	// Line is the source line, counted from zero.
	Line int

	// From and To are BYTE offsets into that line, half-open — which is what
	// the widget wants, and what a scanner naturally produces. Converting to
	// runes at every span is where an accented identifier loses its colour.
	From, To int

	// Kind is what the run looks like.
	Kind Kind
}

// Language is one language's lexical rules.
//
// DATA, not code (Rule 6's shape): a language is a row, and adding one cannot
// add behaviour. That is what keeps this a scanner rather than a place where
// per-language special cases accumulate.
type Language struct {
	// Name is what it is called, for a caller to name it.
	Name string

	// LineComment opens a comment that runs to the end of the line.
	LineComment string

	// BlockOpen and BlockClose bracket a comment that may span lines. Empty
	// means the language has none.
	BlockOpen, BlockClose string

	// Quotes are the characters that open and close a string.
	Quotes string

	// RawQuotes are quotes with no escape processing inside them — Go's
	// backtick, which is why an escaped backslash in one is not an escape.
	RawQuotes string

	// Keywords are the reserved words.
	Keywords map[string]bool

	// Declarers are the keywords whose next name is a DECLARATION.
	Declarers map[string]bool

	// Markdown marks a document scanned by its own line rules rather than as
	// code: its structure is headings, fences and inline marks, not tokens.
	Markdown bool
}

// Scan colours a whole document.
//
// Only PLAIN spans are omitted: a caller sets the theme's ink for everything
// unmarked, so emitting a span per identifier would be a run per word for a
// colour that is already the default.
func Scan(lang Language, source string) []Span {
	if lang.Markdown {
		return scanMarkdown(source)
	}
	var out []Span
	state := &scanner{lang: lang}
	for i, line := range strings.Split(source, "\n") {
		out = append(out, state.line(i, line)...)
	}
	return out
}

// scanner carries what survives between lines: whether a block comment is open.
//
// A block comment is the one thing a line cannot decide alone, which is why
// this is a type with a field rather than a function per line. Getting it wrong
// leaves the rest of a file coloured as code when it is commented out — the
// single most common thing anybody does to a file they are debugging.
//
// declarer is the keyword whose name is still to come — `func` waiting for the
// function's name — and ends with the line or the first brace.
type scanner struct {
	lang     Language
	inBlock  bool
	declarer string
}
