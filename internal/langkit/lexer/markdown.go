package lexer

import "strings"

// scanMarkdown colours a Markdown document as GoLand's editor does: heading
// lines, fenced blocks and inline code, **strong** and *emphasised* text, and
// links. Plain prose carries no span, as plain code does not.
//
// LINE rules, because Markdown's structure is lines: a fence opens on one and
// closes on another, and a heading is a whole line. The fence is the one thing
// a line cannot decide alone, which is why it is carried across them.
func scanMarkdown(source string) []Span {
	var out []Span
	fenced := false
	for i, line := range strings.Split(source, "\n") {
		switch {
		case isFence(line):
			out = append(out, wholeLine(i, line, Code))
			fenced = !fenced
		case fenced:
			if line != "" {
				out = append(out, wholeLine(i, line, Code))
			}
		case isHeading(line):
			out = append(out, wholeLine(i, line, Heading))
		default:
			out = append(out, inlineMarks(i, line)...)
		}
	}
	return out
}

// wholeLine is one span covering a line.
func wholeLine(index int, line string, kind Kind) Span {
	return Span{Line: index, From: 0, To: len(line), Kind: kind}
}

// isFence reports whether a line opens or closes a fenced block.
func isFence(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// isHeading reports whether a line is an ATX heading: one to six hashes and
// then a space or nothing — "#hashtag" is prose.
func isHeading(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	hashes := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
	if hashes < 1 || hashes > 6 {
		return false
	}
	return len(trimmed) == hashes || trimmed[hashes] == ' '
}

// inlineMarks are the code spans, strong and emphasised runs and links in one
// line of prose, left to right. An opener with no closer on the line is prose.
//
// Underscores do NOT mark emphasis here: `snake_case` names are the commonest
// thing in this repository's documents, and colouring half of one as emphasis
// is the highlighter being wrong where a reader is looking hardest.
func inlineMarks(index int, line string) []Span {
	var out []Span
	for at := 0; at < len(line); {
		span, next, ok := markAt(index, line, at)
		if ok {
			out = append(out, span)
			at = next
			continue
		}
		at++
	}
	return out
}

// markAt is the inline mark starting at a byte, if one does, and where the scan
// resumes after it.
func markAt(index int, line string, at int) (Span, int, bool) {
	switch {
	case line[at] == '`':
		return closedBy(index, line, at, "`", Code)
	case strings.HasPrefix(line[at:], "**"):
		return closedBy(index, line, at, "**", Strong)
	case line[at] == '*':
		return closedBy(index, line, at, "*", Emphasis)
	case line[at] == '[':
		return linkAt(index, line, at)
	}
	return Span{}, at, false
}

// closedBy is a run from an opening marker to the next closing one, markers
// included, or nothing where the line never closes it.
func closedBy(index int, line string, at int, marker string, kind Kind) (Span, int, bool) {
	open := at + len(marker)
	end := strings.Index(line[open:], marker)
	if end <= 0 {
		return Span{}, at, false
	}
	to := open + end + len(marker)
	return Span{Line: index, From: at, To: to, Kind: kind}, to, true
}

// linkAt is a `[text](target)` link, brackets included.
func linkAt(index int, line string, at int) (Span, int, bool) {
	mid := strings.Index(line[at:], "](")
	if mid < 0 {
		return Span{}, at, false
	}
	end := strings.IndexByte(line[at+mid:], ')')
	if end < 0 {
		return Span{}, at, false
	}
	to := at + mid + end + 1
	return Span{Line: index, From: at, To: to, Kind: Link}, to, true
}

// FencedLines are the lines of a Markdown document inside a fenced block, the
// fences included: the rows an editor washes, as GoLand's does.
func FencedLines(source string) []int {
	var out []int
	fenced := false
	for i, line := range strings.Split(source, "\n") {
		fence := isFence(line)
		if fence || fenced {
			out = append(out, i)
		}
		if fence {
			fenced = !fenced
		}
	}
	return out
}
