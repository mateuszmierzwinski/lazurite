package language

import (
	"strings"
	"testing"
)

func TestALineOfAServersProseIsFlattenedAndScrubbed(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"plain":             {"undefined: x", "undefined: x"},
		"a newline":         {"line one\nline two", "line one line two"},
		"a carriage return": {"one\r\ntwo", "one two"},
		"a tab":             {"one\ttwo", "one two"},
		"a terminal escape": {"gopls\x1b[2Jcrashed", "gopls[2Jcrashed"},
		"a NUL":             {"a\x00b", "ab"},
		"invalid utf-8":     {"a\xffb", "ab"},
		"a bidi override":   {"safe\u202eelbatsnu", "safeelbatsnu"},
	} {
		if got := CleanLine(c.in); got != c.want {
			t.Fatalf("%s: CleanLine(%q) = %q, want %q", name, c.in, got, c.want)
		}
	}
}

func TestAHoverDocumentKeepsItsLineBreaks(t *testing.T) {
	// A hover answer is Markdown, where a paragraph break carries meaning.
	if got := CleanText("func F()\n\nDoes a thing."); !strings.Contains(got, "\n\n") {
		t.Fatalf("CleanText flattened a paragraph break: %q", got)
	}
	if got := CleanText("a\x00b"); got != "ab" {
		t.Fatalf("CleanText kept a control code: %q", got)
	}
}

func TestOversizedProseIsCutAndSaysSo(t *testing.T) {
	// A reader must not be left believing a sentence ended where it was cut.
	got := CleanLine(strings.Repeat("x", MaxMessage*2))
	if len(got) > MaxMessage+len(Truncated) {
		t.Fatalf("CleanLine returned %d bytes, want at most %d", len(got), MaxMessage)
	}
	if !strings.HasSuffix(got, Truncated) {
		t.Fatalf("a cut message does not say it was cut: %q", got[len(got)-8:])
	}
}

func TestAnEmptyMessageStaysEmpty(t *testing.T) {
	if got := CleanLine(""); got != "" {
		t.Fatalf("CleanLine(\"\") = %q", got)
	}
}

func BenchmarkCleanLine(b *testing.B) {
	for b.Loop() {
		CleanLine("undefined: x (compile)")
	}
}

func BenchmarkCleanText(b *testing.B) {
	source := "func F(a int) error\n\nF does a thing and returns an error.\n"
	for b.Loop() {
		CleanText(source)
	}
}
