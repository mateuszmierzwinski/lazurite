package language

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// What a server is allowed to say, in bytes.
//
// A language server is a subprocess, and everything it sends back is input. A
// message is drawn in a table row and a hover document is rendered as Markdown;
// neither has any business being a megabyte, and a server that sends one is
// either broken or hostile. The bounds are generous enough that no honest
// answer is cut and small enough that no answer costs a frame.
const (
	// MaxMessage bounds one line of prose — a diagnostic, a code action's
	// title, a rename's failure reason.
	MaxMessage = 2000

	// MaxDocument bounds prose that may run to several lines: hover content
	// and signature documentation.
	MaxDocument = 64 << 10
)

// Truncated marks prose that was cut, so a reader is not left believing a
// sentence simply ended there.
const Truncated = "…"

// CleanLine is one line of a server's prose, safe to letter in a list.
//
// Every line break becomes a space, because a row is one line and a message
// carrying a newline would either draw as a box or push the rows below it out
// of the geometry the list hit-tests with.
func CleanLine(text string) string {
	return clean(text, MaxMessage, false)
}

// CleanText is a server's prose that may legitimately run to several lines —
// hover content, a signature's documentation.
//
// Line breaks SURVIVE, because the answer is Markdown and a paragraph break
// carries meaning there. Everything else clean removes is still removed.
func CleanText(text string) string {
	return clean(text, MaxDocument, true)
}

// clean is the general form both facades wrap (Rule 5.1).
//
// Three classes go, and each has been an advisory somewhere: invalid UTF-8,
// which draws as a replacement character or crashes a naive measurer; control
// codes, which a terminal-backed pane would ACT on; and the bidirectional
// overrides of the Trojan Source family, which reorder what a reader sees
// without changing what a compiler reads.
func clean(text string, limit int, keepBreaks bool) string {
	var out strings.Builder
	out.Grow(min(len(text), limit))
	for _, r := range strings.ToValidUTF8(text, "") {
		if out.Len() >= limit {
			return out.String() + Truncated
		}
		out.WriteString(replacement(r, keepBreaks))
	}
	return out.String()
}

// replacement is what one rune contributes, which may be nothing at all.
func replacement(r rune, keepBreaks bool) string {
	switch {
	case r == '\r':
		// Dropped rather than turned into a break of its own: a Windows line
		// ending is CR then LF, and treating both as breaks doubles every one
		// of them — two spaces mid-sentence in a list, a blank line between
		// every line of a hover document.
		return ""
	case r == '\n':
		if keepBreaks {
			return "\n"
		}
		return " "
	case r == '\t':
		return " "
	case reordering(r), unicode.IsControl(r), r == utf8.RuneError:
		return ""
	}
	return string(r)
}

// reordering reports the bidirectional control characters that change the
// order text is READ in without changing the order it is stored in.
//
// The Trojan Source class (CVE-2021-42574). A diagnostic message is drawn
// beside code the operator is about to trust, and one carrying an override can
// make a refusal read as an approval.
func reordering(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x200E || r == 0x200F || r == 0x061C:
		return true
	}
	return false
}
