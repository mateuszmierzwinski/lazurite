package lexer

import "testing"

// kindsOf is each span of a document as (line, text, kind), for a readable
// comparison.
func kindsOf(source string) map[string]Kind {
	lines := splitForTest(source)
	out := map[string]Kind{}
	for _, s := range Scan(markdownLang, source) {
		out[lines[s.Line][s.From:s.To]] = s.Kind
	}
	return out
}

func splitForTest(source string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(source); i++ {
		if source[i] == '\n' {
			lines = append(lines, source[start:i])
			start = i + 1
		}
	}
	return append(lines, source[start:])
}

// TestAMarkdownFileIsKnown — a .md file is coloured, not left plain.
func TestAMarkdownFileIsKnown(t *testing.T) {
	if lang, ok := For("docs/README.md"); !ok || !lang.Markdown {
		t.Fatalf("a .md file resolved to %+v, %v", lang, ok)
	}
}

// TestHeadingsAreWholeLinesAndAHashtagIsNot.
func TestHeadingsAreWholeLinesAndAHashtagIsNot(t *testing.T) {
	got := kindsOf("# Title\n## 1. The short answer\n#hashtag")
	if got["# Title"] != Heading || got["## 1. The short answer"] != Heading {
		t.Fatalf("headings: %v", got)
	}
	if _, ok := got["#hashtag"]; ok {
		t.Fatal("a hashtag was coloured as a heading")
	}
}

// TestAFencedBlockIsCodeUntilItCloses.
func TestAFencedBlockIsCodeUntilItCloses(t *testing.T) {
	got := kindsOf("```bash\ngrep -rn x src\n```\nafter")
	for _, line := range []string{"```bash", "grep -rn x src", "```"} {
		if got[line] != Code {
			t.Fatalf("%q is %v", line, got[line])
		}
	}
	if _, ok := got["after"]; ok {
		t.Fatal("the line after the closing fence is still code")
	}
}

// TestInlineMarksAreFoundWhereTheyClose.
func TestInlineMarksAreFoundWhereTheyClose(t *testing.T) {
	got := kindsOf("in `src/` is **bold** and *soft* and [a link](x.md) plus snake_case_name and `open")
	want := map[string]Kind{"`src/`": Code, "**bold**": Strong, "*soft*": Emphasis, "[a link](x.md)": Link}
	for text, kind := range want {
		if got[text] != kind {
			t.Fatalf("%q is %v in %v", text, got[text], got)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("found %v; an unclosed tick or an underscore was coloured", got)
	}
}

func BenchmarkScanMarkdown(b *testing.B) {
	doc := "# T\n\nSome **bold** `code` and [l](x)\n```go\nfunc main() {}\n```\n"
	for b.Loop() {
		scanMarkdown(doc)
	}
}

// TestADeclaredNameIsItsOwnKind — GoLand sets the name a declaration
// introduces apart from every use of it: the function after `func` (past a
// method's receiver) and the type after `type`, and nothing else on the line.
func TestADeclaredNameIsItsOwnKind(t *testing.T) {
	lang, _ := For("x.go")
	source := "func (a Adapter) matches(model string) bool {\ntype Adapter struct {\nfunc main() { run(x) }"
	lines := splitForTest(source)
	var declared []string
	for _, s := range Scan(lang, source) {
		if s.Kind == Declaration {
			declared = append(declared, lines[s.Line][s.From:s.To])
		}
	}
	want := []string{"matches", "Adapter", "main"}
	if len(declared) != len(want) {
		t.Fatalf("declared %v, want %v", declared, want)
	}
	for i := range want {
		if declared[i] != want[i] {
			t.Fatalf("declared %v, want %v", declared, want)
		}
	}
}

// TestFencedLinesAreTheBlockAndItsFences.
func TestFencedLinesAreTheBlockAndItsFences(t *testing.T) {
	got := FencedLines("text\n```go\nx := 1\n```\nafter\n~~~\nopen to the end")
	want := []int{1, 2, 3, 5, 6}
	if len(got) != len(want) {
		t.Fatalf("fenced %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fenced %v, want %v", got, want)
		}
	}
}

func BenchmarkFencedLines(b *testing.B) {
	doc := "t\n```\na\n```\n"
	for b.Loop() {
		FencedLines(doc)
	}
}
