package lexer

import (
	"strings"
	"testing"
)

// coloured is what the scanner made of a source, as a readable string: each
// span as its kind and its text. Asserting on offsets alone hides the case
// where the kinds are right and the boundaries are off by one.
func coloured(lang Language, source string) []string {
	lines := strings.Split(source, "\n")
	var out []string
	for _, s := range Scan(lang, source) {
		out = append(out, kindName(s.Kind)+"("+lines[s.Line][s.From:s.To]+")")
	}
	return out
}

func kindName(k Kind) string {
	switch k {
	case Comment:
		return "comment"
	case String:
		return "string"
	case Number:
		return "number"
	case Keyword:
		return "keyword"
	default:
		return "plain"
	}
}

func has(spans []string, want string) bool {
	for _, s := range spans {
		if s == want {
			return true
		}
	}
	return false
}

// TestTheFourThingsThatMatterAreColoured.
//
// Colouring exists so the eye can skip: a string is not code, a comment is not
// either, and a keyword is where a statement begins.
func TestTheFourThingsThatMatterAreColoured(t *testing.T) {
	got := coloured(goLang, "func main() { // start\n\ts := \"hello\"\n\tn := 42\n}")

	for _, want := range []string{
		`keyword(func)`, `comment(// start)`, `string("hello")`, `number(42)`,
	} {
		if !has(got, want) {
			t.Errorf("%s is missing from %v", want, got)
		}
	}
}

// TestPlainCodeIsNotSpanned.
//
// A caller letters everything unmarked in the theme's ink, so a span per
// identifier would be a run per word for the colour that is already the
// default.
func TestPlainCodeIsNotSpanned(t *testing.T) {
	for _, s := range Scan(goLang, "x := someFunction(other, thing)") {
		if s.Kind == Plain {
			t.Errorf("a plain span was emitted at %d..%d", s.From, s.To)
		}
	}
}

// TestABlockCommentSurvivesAcrossLines.
//
// The one thing a line cannot decide alone. Getting it wrong leaves the rest of
// a file coloured as code when it is commented out — the single most common
// thing anybody does to a file they are debugging.
func TestABlockCommentSurvivesAcrossLines(t *testing.T) {
	source := "before\n/* open\nstill inside\nfunc notCode() {\nclose */\nafter"
	spans := Scan(goLang, source)

	commented := map[int]bool{}
	for _, s := range spans {
		if s.Kind == Comment {
			commented[s.Line] = true
		}
	}
	for _, line := range []int{1, 2, 3, 4} {
		if !commented[line] {
			t.Errorf("line %d is not inside the block comment", line)
		}
	}
	for _, s := range spans {
		if s.Kind == Keyword && s.Line == 3 {
			t.Error("a keyword inside a block comment was coloured as code")
		}
	}
	if commented[5] {
		t.Error("the line after the close is still commented")
	}
}

// TestABlockCommentThatClosesOnItsOwnLineDoesNotLeak.
func TestABlockCommentThatClosesOnItsOwnLineDoesNotLeak(t *testing.T) {
	spans := Scan(goLang, "/* here */ func real() {}")
	var keywords int
	for _, s := range spans {
		if s.Kind == Keyword {
			keywords++
		}
	}
	if keywords != 1 {
		t.Fatalf("%d keywords after a closed comment: %v", keywords,
			coloured(goLang, "/* here */ func real() {}"))
	}
}

// TestAnUnterminatedStringStopsAtTheEndOfItsLine.
//
// A scanner that carried the state across would colour the rest of the file as
// a string the moment somebody typed a lone quote — which is what happens while
// a line is being written.
func TestAnUnterminatedStringStopsAtTheEndOfItsLine(t *testing.T) {
	spans := Scan(goLang, "s := \"unclosed\nfunc stillCode() {}")
	for _, s := range spans {
		if s.Kind == String && s.Line != 0 {
			t.Errorf("the string leaked onto line %d", s.Line)
		}
	}
	found := false
	for _, s := range spans {
		if s.Kind == Keyword && s.Line == 1 {
			found = true
		}
	}
	if !found {
		t.Error("the line after an unterminated string is not code")
	}
}

// TestAnEscapedQuoteDoesNotEndTheString.
func TestAnEscapedQuoteDoesNotEndTheString(t *testing.T) {
	got := coloured(goLang, `s := "a \" b" + tail`)
	if !has(got, `string("a \" b")`) {
		t.Fatalf("the escaped quote ended the string: %v", got)
	}
}

// TestARawStringHasNoEscapes.
//
// Go's backtick string ends at the next backtick whatever precedes it, so a
// trailing backslash inside one does not escape anything.
func TestARawStringHasNoEscapes(t *testing.T) {
	got := coloured(goLang, "s := `a \\` + tail")
	if !has(got, "string(`a \\`)") {
		t.Fatalf("a backslash escaped inside a raw string: %v", got)
	}
}

// TestAKeywordInsideAnIdentifierIsNotColoured.
//
// `iffy` and `forEach` are not statements, and colouring them makes the eye
// stop where nothing is happening.
func TestAKeywordInsideAnIdentifierIsNotColoured(t *testing.T) {
	for _, s := range Scan(goLang, "iffy := forEach(returnValue, mapping)") {
		if s.Kind == Keyword {
			t.Errorf("a keyword was found in %d..%d", s.From, s.To)
		}
	}
}

// TestADigitInsideAnIdentifierIsNotANumber.
func TestADigitInsideAnIdentifierIsNotANumber(t *testing.T) {
	for _, s := range Scan(goLang, "x1 := utf8Len + sha256Sum") {
		if s.Kind == Number {
			t.Errorf("a number was found in %d..%d", s.From, s.To)
		}
	}
}

// TestANumberIsOneSpanHoweverItIsWritten.
//
// `0xff` split into three spans reads as a number, a word and a number.
func TestANumberIsOneSpanHoweverItIsWritten(t *testing.T) {
	for source, want := range map[string]string{
		"n := 0xff":   "number(0xff)",
		"n := 1_000":  "number(1_000)",
		"n := 1.5e10": "number(1.5e10)",
		"n := 0b1010": "number(0b1010)",
		"n := 42":     "number(42)",
	} {
		if got := coloured(goLang, source); !has(got, want) {
			t.Errorf("%q coloured as %v, want %s", source, got, want)
		}
	}
}

// TestAnAccentedIdentifierIsOneWord.
//
// Every byte over 0x7f counts as a word byte, or a UTF-8 name is coloured in
// fragments.
func TestAnAccentedIdentifierIsOneWord(t *testing.T) {
	got := Scan(goLang, "func obsługa() {}")
	for _, s := range got {
		if s.Kind == Keyword && s.From != 0 {
			t.Errorf("a fragment of the identifier was coloured at %d..%d", s.From, s.To)
		}
	}
}

// TestEverySpanIsInsideItsLine.
//
// The widget slices the line by these offsets. One past the end panics the
// window, and the source is a file somebody else wrote.
func TestEverySpanIsInsideItsLine(t *testing.T) {
	sources := []string{
		"", "\n\n\n", "/*", "\"", "`", "//", "0x", "'", "a\n\"\n*/\n`",
		strings.Repeat("/*\n", 50), strings.Repeat("\"", 50),
		"\x00\x01\x02", "ł", "func ł() { /* ", "'\\",
	}
	for _, lang := range []Language{goLang, cLang, jsLang, jsonLang, cssLang,
		shellLang, pythonLang, luaLang} {
		for _, source := range sources {
			assertSpansFit(t, lang, source)
		}
	}
}

// assertSpansFit checks every span of one source against its own line.
func assertSpansFit(t *testing.T, lang Language, source string) {
	t.Helper()
	lines := strings.Split(source, "\n")
	for _, s := range Scan(lang, source) {
		if s.Line < 0 || s.Line >= len(lines) {
			t.Fatalf("%s: span on line %d of %d", lang.Name, s.Line, len(lines))
		}
		if s.From < 0 || s.To > len(lines[s.Line]) || s.From > s.To {
			t.Fatalf("%s: span %d..%d in a %d-byte line",
				lang.Name, s.From, s.To, len(lines[s.Line]))
		}
	}
}

// TestScanningAlwaysTerminates.
//
// A scanner that failed to advance would hang the window on a file, and the
// file comes from whatever the operator opened.
func TestScanningAlwaysTerminates(t *testing.T) {
	for _, source := range []string{
		strings.Repeat("`", 1000), strings.Repeat("/*", 1000),
		strings.Repeat("\\", 1000), strings.Repeat("\"\\", 500),
	} {
		Scan(goLang, source)
	}
}

// TestTheLanguageIsChosenByExtension.
func TestTheLanguageIsChosenByExtension(t *testing.T) {
	for path, want := range map[string]string{
		"main.go": "Go", "a.c": "C", "a.hpp": "C", "x.js": "JavaScript",
		"p.json": "JSON", "s.css": "CSS", "r.sh": "Shell", "b.py": "Python",
		"BUILD.bazel": "Python", "rules.bzl": "Python", "s.lua": "Lua",
		"MAIN.GO": "Go",
	} {
		got, ok := For(path)
		if !ok {
			t.Errorf("%q was not recognised", path)
			continue
		}
		if got.Name != want {
			t.Errorf("%q is %q, want %q", path, got.Name, want)
		}
	}
}

// TestAnUnknownExtensionIsReportedRatherThanGuessed.
//
// Plain text is a correct answer for a file nothing here understands, and a
// wrong colouring is worse than none.
func TestAnUnknownExtensionIsReportedRatherThanGuessed(t *testing.T) {
	for _, path := range []string{"notes.txt", "Makefile", "a.rs", "", "x."} {
		if _, ok := For(path); ok {
			t.Errorf("%q was given a language", path)
		}
	}
}

// TestEveryLanguageIsNamed, since a table row with no name cannot be reported.
func TestEveryLanguageIsNamed(t *testing.T) {
	for ext, lang := range byExtension {
		if lang.Name == "" {
			t.Errorf("%q has no name", ext)
		}
		// A CODE language with no keywords is a row somebody forgot to fill
		// in; Markdown has none by design — it is scanned by line rules.
		if len(lang.Keywords) == 0 && !lang.Markdown {
			t.Errorf("%s has no keywords", lang.Name)
		}
	}
	if len(Known()) == 0 {
		t.Fatal("no languages are reported")
	}
}

func BenchmarkScanAGoFile(b *testing.B) {
	source := strings.Repeat("func name() { // note\n\ts := \"x\"\n\tn := 42\n}\n", 100)
	b.ResetTimer()
	for b.Loop() {
		Scan(goLang, source)
	}
}
