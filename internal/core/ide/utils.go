package ide

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
)

// displayName is a path as it is shown: relative to the project when it is
// inside it, whole when it is not.
func displayName(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || root == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}

// placePattern is a place as the Go tool prints one: a file, a line, and
// sometimes a column, before the message.
var placePattern = regexp.MustCompile(`^\s*([^\s:]+\.go):(\d+)(?::(\d+))?`)

// parsePlace reads the place a line of the Go tool's output names: a
// compiler's `main.go:12:5: …`, a test's `    main_test.go:15: …`.
func parsePlace(text string) (file string, line, col int, ok bool) {
	m := placePattern.FindStringSubmatch(text)
	if m == nil {
		return "", 0, 0, false
	}
	line, err := strconv.Atoi(m[2])
	if err != nil || line < 1 {
		return "", 0, 0, false
	}
	col, _ = strconv.Atoi(m[3])
	return m[1], line, col, true
}

// mainPattern is the line a program begins on.
var mainPattern = regexp.MustCompile(`(?m)^func main\(\)`)

// mainLine is the line of a text's `func main`, counted from zero.
func mainLine(text string) (int, bool) {
	at := mainPattern.FindStringIndex(text)
	if at == nil || !declaresMain(text) {
		return 0, false
	}
	return strings.Count(text[:at[0]], "\n"), true
}

// packagePattern is a package clause naming main.
var packagePattern = regexp.MustCompile(`(?m)^package main\b`)

// declaresMain reports whether a text is a file of package main.
func declaresMain(text string) bool {
	return packagePattern.MatchString(text)
}

// relocate is where a line of an old text is in a new one that differs
// from it somewhere else: the same line when what is above it is
// unchanged, the same distance from the end when what is below it is.
// Cleaning a file up adds and removes imports above the caret; this keeps
// the caret on the line it was on.
func relocate(old, fresh []string, line int) int {
	if line < commonPrefix(old, fresh) {
		return line
	}
	fromEnd := len(old) - 1 - line
	if fromEnd < commonSuffix(old, fresh) {
		return max(len(fresh)-1-fromEnd, 0)
	}
	return min(max(line+len(fresh)-len(old), 0), max(len(fresh)-1, 0))
}

// commonPrefix is how many leading lines two texts share.
func commonPrefix(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// commonSuffix is how many trailing lines two texts share.
func commonSuffix(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	return n
}

// maxStatus bounds a message on the status line.
const maxStatus = 200

// oneLine is a text made fit to show on one line: its first line, without
// control characters, not too long. What a compiler, a debugger or a file
// said must not be able to send the terminal a command.
func oneLine(text string) string {
	text, _, _ = strings.Cut(strings.TrimSpace(text), "\n")
	var sb strings.Builder
	for _, r := range text {
		if sb.Len() >= maxStatus {
			sb.WriteRune('…')
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// expandTabs is a line with its tabs opened out to the editor's tab stops.
func expandTabs(text string) string {
	if !strings.Contains(text, "\t") {
		return text
	}
	var sb strings.Builder
	col := 0
	for _, r := range text {
		if r != '\t' {
			sb.WriteRune(r)
			col++
			continue
		}
		n := edit.TabWidth - col%edit.TabWidth
		sb.WriteString(strings.Repeat(" ", n))
		col += n
	}
	return sb.String()
}

// maxWrapped bounds how many lines a wrapped text becomes.
const maxWrapped = 400

// wrap lays a text out in lines no wider than a width, breaking at spaces;
// its own line breaks are kept.
func wrap(text string, width int) []string {
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		out = append(out, wrapLine(expandTabs(para), max(width, 1))...)
		if len(out) >= maxWrapped {
			return append(out[:maxWrapped], "…")
		}
	}
	return out
}

// wrapLine lays one paragraph out in lines no wider than a width.
func wrapLine(para string, width int) []string {
	runes := []rune(para)
	if len(runes) <= width {
		return []string{para}
	}
	var out []string
	for len(runes) > width {
		cut := width
		for i := width; i > width/2; i-- {
			if runes[i] == ' ' {
				cut = i
				break
			}
		}
		out = append(out, string(runes[:cut]))
		runes = []rune(strings.TrimLeft(string(runes[cut:]), " "))
	}
	return append(out, string(runes))
}

// lineAndColumn reads "12" or "12:5" as a position, both counted from one
// as a person counts them.
func lineAndColumn(typed string) (edit.Pos, bool) {
	left, right, _ := strings.Cut(strings.TrimSpace(typed), ":")
	line, err := strconv.Atoi(strings.TrimSpace(left))
	if err != nil || line < 1 {
		return edit.Pos{}, false
	}
	col, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil || col < 1 {
		col = 1
	}
	return edit.Pos{Line: line - 1, Col: col - 1}, true
}
