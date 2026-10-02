package gopls

import (
	"sort"
	"strings"

	"github.com/mateuszmierzwinski/lazurite/internal/core/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// The two ways of counting a column. The editor counts runes; the protocol
// counts UTF-16 code units, of which a character outside the basic plane —
// an emoji — is two. Every position crossing this package is converted.

// lineOf is one line of a text; empty when the text has no such line.
func lineOf(text string, line int) string {
	for ; line > 0; line-- {
		_, rest, found := strings.Cut(text, "\n")
		if !found {
			return ""
		}
		text = rest
	}
	first, _, _ := strings.Cut(text, "\n")
	return first
}

// position is an editor's place as the protocol counts it.
func position(text string, at edit.Pos) language.Position {
	units, col := 0, 0
	for _, r := range lineOf(text, at.Line) {
		if col >= at.Col {
			break
		}
		units += unitsOf(r)
		col++
	}
	return language.Position{Line: at.Line, Character: units}
}

// unitsOf is how many UTF-16 code units a rune takes.
func unitsOf(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

// caret is the protocol's place as the editor counts it. With no text to
// count in — a file that is not open — the column is taken as it is,
// which is right for every line without an emoji before it.
func caret(text string, at language.Position) edit.Pos {
	if text == "" {
		return edit.Pos{Line: max(at.Line, 0), Col: max(at.Character, 0)}
	}
	units, col := 0, 0
	for _, r := range lineOf(text, at.Line) {
		if units >= at.Character {
			break
		}
		units += unitsOf(r)
		col++
	}
	return edit.Pos{Line: max(at.Line, 0), Col: col}
}

// problems is the server's diagnostics as the IDE's problems. Hints are
// left out: they are suggestions, not faults.
func problems(text string, found []language.Diagnostic) []ide.Problem {
	var out []ide.Problem
	for _, d := range found {
		if d.Severity > language.SeverityWarning {
			continue
		}
		p := ide.Problem{
			From: caret(text, d.Range.Start), To: caret(text, d.Range.End),
			Severe: d.Severity == language.SeverityError, Message: d.Message,
		}
		if p.To == p.From {
			p.To.Col++
		}
		out = append(out, p)
	}
	return out
}

// suggestions is the server's completions as the IDE's, in the order the
// server ranked them.
func suggestions(text string, items []language.Completion) []ide.Suggestion {
	sorted := append([]language.Completion(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].SortKey() < sorted[j].SortKey() })
	out := make([]ide.Suggestion, 0, len(sorted))
	for _, item := range sorted {
		out = append(out, suggestion(text, item))
	}
	return out
}

// suggestion is one completion as the IDE's: what to show, what to put in
// place of the word, and what else choosing it changes.
func suggestion(text string, item language.Completion) ide.Suggestion {
	s := ide.Suggestion{Label: item.Label, Detail: item.Detail, Insert: item.Insert}
	if s.Detail == "" {
		s.Detail = item.Kind.String()
	}
	if s.Insert == "" {
		s.Insert = item.Label
	}
	for _, e := range item.Additional {
		s.Extra = append(s.Extra, ide.Edit{From: caret(text, e.Span.Start), To: caret(text, e.Span.End), Text: e.NewText})
	}
	return s
}
