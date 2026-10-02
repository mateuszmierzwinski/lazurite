package language

import (
	"sort"
	"strings"
)

// Apply performs a server's edits on a document's text.
//
// The protocol's rules, and both of them are load-bearing:
//
//   - Every edit's range is against the ORIGINAL text, so they are applied back
//     to front. Applied in order, the second edit's offsets refer to a document
//     the first one has already moved.
//   - Edits in one set may not overlap. Two that do are a server fault, and
//     splicing them anyway silently produces a file nobody wrote — so it is
//     refused with a named error instead.
//
// A position counts UTF-16 code units (see Position), which is why this cannot
// be done with byte or rune arithmetic: a single emoji in a line above the edit
// moves every offset after it by a different amount in each of the three
// countings.
func Apply(text string, edits []TextEdit) (string, error) {
	spans, err := spansOf(text, edits)
	if err != nil {
		return "", err
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].from > spans[j].from })
	if overlapping(spans) {
		return "", ErrOverlappingEdits
	}
	out := text
	for _, span := range spans {
		out = out[:span.from] + span.text + out[span.to:]
	}
	return out, nil
}

// byteSpan is one edit resolved to byte offsets in the document.
type byteSpan struct {
	from int
	to   int
	text string
}

// spansOf resolves every edit's range to byte offsets, refusing one that does
// not fit the document.
//
// An edit past the end is REFUSED rather than clamped. Clamping would apply a
// rename to the wrong place and report success, and a rename that quietly wrote
// somewhere else is worse than one that did not run.
func spansOf(text string, edits []TextEdit) ([]byteSpan, error) {
	starts := lineStarts(text)
	out := make([]byteSpan, 0, len(edits))
	for _, edit := range edits {
		from, ok := offsetOf(text, starts, edit.Span.Start)
		to, alsoOK := offsetOf(text, starts, edit.Span.End)
		if !ok || !alsoOK || to < from {
			return nil, ErrEditOutOfRange
		}
		out = append(out, byteSpan{from: from, to: to, text: edit.NewText})
	}
	return out, nil
}

// overlapping reports whether any two edits touch the same text, given the
// spans sorted from last to first.
func overlapping(spans []byteSpan) bool {
	for i := 1; i < len(spans); i++ {
		if spans[i].to > spans[i-1].from {
			return true
		}
	}
	return false
}

// lineStarts is the byte offset each line begins at.
//
// Computed once for a whole edit set rather than per edit: a rename touching a
// hundred places in one file would otherwise walk the document a hundred times.
func lineStarts(text string) []int {
	starts := []int{0}
	for at := 0; ; {
		next := strings.IndexByte(text[at:], '\n')
		if next < 0 {
			return starts
		}
		at += next + 1
		starts = append(starts, at)
	}
}

// offsetOf is a protocol position's byte offset in the document.
//
// A position PAST the end of its line is answered with the line's end, which is
// the protocol's own instruction — servers routinely name a column beyond the
// last character to mean "the end of this line". A position past the last line
// is not a position and is refused.
func offsetOf(text string, starts []int, at Position) (int, bool) {
	if at.Line < 0 || at.Line >= len(starts) {
		return 0, false
	}
	start := starts[at.Line]
	return start + Column(lineAt(text, starts, at.Line), at.Character), true
}

// lineAt is one line's text, its terminator excluded.
func lineAt(text string, starts []int, line int) string {
	if line+1 < len(starts) {
		return strings.TrimSuffix(text[starts[line]:starts[line+1]-1], "\r")
	}
	return text[starts[line]:]
}

// Column is how many BYTES into a line a UTF-16 code-unit count reaches.
//
// The conversion the protocol forces on every client. A rune below U+10000 is
// one code unit and an astral one is two, so a line with an emoji in it counts
// differently in every one of bytes, runes and code units — and an editor that
// used the wrong one puts a rename's edit a character or two off, in exactly
// the files a European or emoji-carrying comment appears in.
func Column(line string, units int) int {
	if units <= 0 {
		return 0
	}
	counted := 0
	for at, r := range line {
		if counted >= units {
			return at
		}
		counted += utf16Units(r)
	}
	return len(line)
}

// PositionOf is where a RUNE offset into a document falls, in the protocol's
// own counting.
//
// The inverse of what Apply does, and needed for the same reason: a widget
// counts its caret in runes, and the protocol counts in UTF-16 code units, so
// every question asked at the caret has to be converted. An offset past the end
// answers with the end, because a caret cannot be outside the text it is in.
func PositionOf(text string, offset int) Position {
	line, column := 0, 0
	for at, r := range []rune(text) {
		if at >= offset {
			break
		}
		if r == '\n' {
			line, column = line+1, 0
			continue
		}
		column += utf16Units(r)
	}
	return Position{Line: line, Character: column}
}

// utf16Units is how many UTF-16 code units one rune takes: two above the basic
// plane, one below it.
func utf16Units(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

// RuneOffset is where a position falls, counted in RUNES — the inverse of
// PositionOf.
//
// Runes because that is what a text widget counts its caret in, and the whole
// reason both directions exist: the protocol counts UTF-16 code units, the
// editor counts runes, and the conversion has to happen at every boundary
// between them or a caret lands a character or two out in exactly the files
// with an accent or an emoji above it.
//
// A position past the end answers with the end, for the reason PositionOf
// clamps the other way: a caret cannot be outside the text it is in.
func RuneOffset(text string, at Position) int {
	runes := []rune(text)
	line, units, offset := 0, 0, 0
	for offset < len(runes) {
		if line == at.Line && units >= at.Character {
			return offset
		}
		if runes[offset] == '\n' {
			if line == at.Line {
				return offset
			}
			line, units = line+1, 0
		} else if line == at.Line {
			units += utf16Units(runes[offset])
		}
		offset++
	}
	return len(runes)
}

// Moved is how far a position shifts once a set of edits BEFORE it has been
// applied: the sum of what each of them adds or takes away.
//
// It exists for the caret. A completion that brings an import with it changes
// the file above the word being typed, so the offset the caret should land at
// is the one it would have had plus everything the earlier edits grew by —
// and an editor that ignored that puts the caret a line and a half from where
// the operator was writing.
//
// Edits at or after the position are ignored, and so is one that does not fit
// the document: this answers about a caret, and a position nobody can resolve
// is not worth refusing a completion over.
func Moved(text string, edits []TextEdit, at Position) int {
	target, shift := RuneOffset(text, at), 0
	for _, edit := range edits {
		from, to := RuneOffset(text, edit.Span.Start), RuneOffset(text, edit.Span.End)
		if to > target || to < from {
			continue
		}
		shift += len([]rune(edit.NewText)) - (to - from)
	}
	return shift
}
