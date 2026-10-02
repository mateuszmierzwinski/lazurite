package lexer

import (
	"path/filepath"
	"strings"
)

// For is the language a filename is written in, and whether it is known.
//
// By EXTENSION, which is what an editor has: the content would be a better
// signal and reading it costs a heuristic per language, and getting it wrong
// colours a file as something it is not. An unknown extension is reported as
// such rather than guessed at — plain text is a correct answer for a file
// nothing here understands, and a wrong colouring is worse than none.
func For(path string) (Language, bool) {
	lang, ok := byExtension[strings.ToLower(filepath.Ext(path))]
	return lang, ok
}

// Known are the languages this understands, for a caller to report.
func Known() []string {
	seen := map[string]bool{}
	var out []string
	for _, lang := range byExtension {
		if !seen[lang.Name] {
			seen[lang.Name] = true
			out = append(out, lang.Name)
		}
	}
	sortNames(out)
	return out
}

// byExtension is the table.
//
// Deliberately SHORT. These are the languages this monorepo is written in plus
// the two configuration formats beside them, and a language nobody here reads
// is a row of keywords to keep current for nothing. Adding one is a row.
var byExtension = map[string]Language{
	".go":       goLang,
	".c":        cLang,
	".h":        cLang,
	".cc":       cLang,
	".cpp":      cLang,
	".hpp":      cLang,
	".js":       jsLang,
	".json":     jsonLang,
	".css":      cssLang,
	".sh":       shellLang,
	".bash":     shellLang,
	".py":       pythonLang,
	".lua":      luaLang,
	".bzl":      pythonLang,
	".bazel":    pythonLang,
	".md":       markdownLang,
	".markdown": markdownLang,
}

// markdownLang is Markdown, scanned by its own line rules (markdown.go).
var markdownLang = Language{Name: "Markdown", Markdown: true}

// goLang is Go, which is what most of this repository is.
var goLang = Language{
	Declarers: map[string]bool{"func": true, "type": true},
	Name:      "Go", LineComment: "//", BlockOpen: "/*", BlockClose: "*/",
	Quotes: `"'`, RawQuotes: "`",
	Keywords: wordSet(`break case chan const continue default defer else
		fallthrough for func go goto if import interface map package range
		return select struct switch type var nil true false iota
		bool byte complex64 complex128 error float32 float64 int int8 int16
		int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any`),
}

// cLang covers C and C++, which share enough of a lexical surface that two
// rows would differ only in keywords nobody would notice.
var cLang = Language{
	// `struct`, `union`, `enum` and `typedef` introduce a NAME; a plain C
	// function definition does not — `int main(void)` is led by a type, not
	// by a keyword — so a function header in C is not marked by this
	// mechanism and is left for a parser that knows the language.
	Declarers: map[string]bool{"struct": true, "union": true, "enum": true, "typedef": true},
	Name:      "C", LineComment: "//", BlockOpen: "/*", BlockClose: "*/",
	Quotes: `"'`,
	Keywords: wordSet(`auto break case char const continue default do double
		else enum extern float for goto if inline int long register restrict
		return short signed sizeof static struct switch typedef union unsigned
		void volatile while bool true false NULL class namespace template
		public private protected virtual new delete this nullptr using`),
}

// jsLang is JavaScript, which the toolkit's own rules say is written as plain
// files with no bundler.
var jsLang = Language{
	Declarers: map[string]bool{"function": true, "class": true},
	Name:      "JavaScript", LineComment: "//", BlockOpen: "/*", BlockClose: "*/",
	Quotes: `"'`, RawQuotes: "`",
	Keywords: wordSet(`async await break case catch class const continue
		debugger default delete do else export extends finally for function if
		import in instanceof let new of return static super switch this throw
		try typeof var void while with yield null true false undefined`),
}

// jsonLang has no comments and no keywords beyond its three literals.
var jsonLang = Language{
	Name: "JSON", Quotes: `"`,
	Keywords: wordSet(`true false null`),
}

// cssLang has block comments only.
var cssLang = Language{
	Name: "CSS", BlockOpen: "/*", BlockClose: "*/", Quotes: `"'`,
	Keywords: wordSet(`important inherit initial unset none auto var calc`),
}

// shellLang's `#` comment is the reason LineComment is a string rather than a
// character.
var shellLang = Language{
	Name: "Shell", LineComment: "#", Quotes: `"'`,
	Keywords: wordSet(`if then else elif fi for while until do done case esac
		function return local export readonly declare in select time coproc`),
}

// pythonLang also covers Starlark, which is what a BUILD file is.
var pythonLang = Language{
	Name: "Python", LineComment: "#", Quotes: `"'`,
	Keywords: wordSet(`and as assert async await break class continue def del
		elif else except finally for from global if import in is lambda load
		None nonlocal not or pass raise return True False try while with yield`),
}

// luaLang is what embedded scripts are written in.
var luaLang = Language{
	Name: "Lua", LineComment: "--", BlockOpen: "--[[", BlockClose: "]]",
	Quotes: `"'`,
	Keywords: wordSet(`and break do else elseif end false for function goto if
		in local nil not or repeat return then true until while`),
}

// wordSet turns a whitespace-separated list into a set, so a language is one
// readable block rather than a column of quoted strings.
func wordSet(words string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(words) {
		out[w] = true
	}
	return out
}

// sortNames is an insertion sort over a handful of names.
func sortNames(in []string) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j] < in[j-1]; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}
