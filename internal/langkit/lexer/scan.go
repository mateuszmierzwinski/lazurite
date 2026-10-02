package lexer

import "strings"

// line colours one line, carrying the block-comment state across.
func (s *scanner) line(index int, text string) []Span {
	var out []Span
	at := 0
	s.declarer = ""
	for at < len(text) {
		span, next := s.spanAt(index, text, at)
		if span.To > span.From {
			out = append(out, span)
		}
		if next <= at {
			next = at + 1
		}
		at = next
	}
	return out
}

// spanAt reads whatever begins at `at`, and where the next one starts.
func (s *scanner) spanAt(index int, text string, at int) (Span, int) {
	if s.inBlock {
		return s.blockRest(index, text, at)
	}
	switch {
	case s.opensBlock(text, at):
		return s.blockFrom(index, text, at)
	case s.opensLineComment(text, at):
		return Span{Line: index, From: at, To: len(text), Kind: Comment}, len(text)
	case s.quoteAt(text, at) != 0:
		return s.stringFrom(index, text, at)
	case isDigit(text[at]) && startsToken(text, at):
		return s.numberFrom(index, text, at)
	case isWordByte(text[at]):
		return s.wordFrom(index, text, at)
	default:
		if text[at] == '{' {
			s.declarer = ""
		}
		return Span{Line: index, From: at, To: at}, at + 1
	}
}

// blockRest colours the continuation of a block comment opened on an earlier
// line, and closes it if the terminator is here.
func (s *scanner) blockRest(index int, text string, at int) (Span, int) {
	end := strings.Index(text[at:], s.lang.BlockClose)
	if end < 0 {
		return Span{Line: index, From: at, To: len(text), Kind: Comment}, len(text)
	}
	s.inBlock = false
	to := at + end + len(s.lang.BlockClose)
	return Span{Line: index, From: at, To: to, Kind: Comment}, to
}

// blockFrom colours a block comment opening here.
func (s *scanner) blockFrom(index int, text string, at int) (Span, int) {
	rest := text[at+len(s.lang.BlockOpen):]
	end := strings.Index(rest, s.lang.BlockClose)
	if end < 0 {
		s.inBlock = true
		return Span{Line: index, From: at, To: len(text), Kind: Comment}, len(text)
	}
	to := at + len(s.lang.BlockOpen) + end + len(s.lang.BlockClose)
	return Span{Line: index, From: at, To: to, Kind: Comment}, to
}

// stringFrom colours a string literal.
//
// An UNTERMINATED one colours to the end of the line and no further. A string
// is usually closed on its own line, and a scanner that carried the state
// across would colour the rest of the file as a string the moment somebody
// typed a lone quote — which is exactly what happens while a line is being
// written.
func (s *scanner) stringFrom(index int, text string, at int) (Span, int) {
	quote := s.quoteAt(text, at)
	escapes := !strings.ContainsRune(s.lang.RawQuotes, rune(quote))
	for i := at + 1; i < len(text); i++ {
		if escapes && text[i] == '\\' {
			i++
			continue
		}
		if text[i] == quote {
			return Span{Line: index, From: at, To: i + 1, Kind: String}, i + 1
		}
	}
	return Span{Line: index, From: at, To: len(text), Kind: String}, len(text)
}

// numberFrom colours a numeric literal, taking the hex, exponent and separator
// characters with it so `0xff` and `1_000` are one span rather than three.
func (s *scanner) numberFrom(index int, text string, at int) (Span, int) {
	i := at
	for i < len(text) && isNumberByte(text[i]) {
		i++
	}
	return Span{Line: index, From: at, To: i, Kind: Number}, i
}

// wordFrom colours an identifier, which is a keyword only if the language says
// so.
func (s *scanner) wordFrom(index int, text string, at int) (Span, int) {
	i := at
	for i < len(text) && isWordByte(text[i]) {
		i++
	}
	word := text[at:i]
	if s.lang.Keywords[word] {
		if s.lang.Declarers[word] {
			s.declarer = word
		}
		return Span{Line: index, From: at, To: i, Kind: Keyword}, i
	}
	if s.declares(text, i) {
		s.declarer = ""
		return Span{Line: index, From: at, To: i, Kind: Declaration}, i
	}
	return Span{Line: index, From: at, To: at}, i
}

// declares reports whether the word ending at `end` is the name a pending
// declaration introduces: the first name after `type`, and after `func` the
// first one followed straight by "(" or "[" — which passes over a method's
// receiver, `(r *T)`, to the name after it.
func (s *scanner) declares(text string, end int) bool {
	switch s.declarer {
	case "":
		return false
	case "func":
		return end < len(text) && (text[end] == '(' || text[end] == '[')
	default:
		return true
	}
}

// opensBlock reports a block comment starting here.
func (s *scanner) opensBlock(text string, at int) bool {
	return s.lang.BlockOpen != "" && strings.HasPrefix(text[at:], s.lang.BlockOpen)
}

// opensLineComment reports a line comment starting here.
func (s *scanner) opensLineComment(text string, at int) bool {
	return s.lang.LineComment != "" && strings.HasPrefix(text[at:], s.lang.LineComment)
}

// quoteAt is the quote character opening here, or zero.
func (s *scanner) quoteAt(text string, at int) byte {
	c := text[at]
	if strings.IndexByte(s.lang.Quotes, c) >= 0 || strings.IndexByte(s.lang.RawQuotes, c) >= 0 {
		return c
	}
	return 0
}

// startsToken reports that a byte begins a token rather than continuing one,
// so the `1` in `x1` is not read as a number.
func startsToken(text string, at int) bool {
	return at == 0 || !isWordByte(text[at-1])
}

// isWordByte reports a byte that may appear in an identifier.
//
// Every byte over 0x7f counts, so a UTF-8 identifier is one word rather than
// one span per continuation byte — which would colour an accented name in
// fragments.
func isWordByte(c byte) bool {
	return c == '_' || c >= 0x80 ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || isDigit(c)
}

// isNumberByte reports a byte that may appear in a numeric literal.
func isNumberByte(c byte) bool {
	return isDigit(c) || c == '.' || c == '_' || c == 'x' || c == 'X' ||
		c == 'b' || c == 'o' || c == 'e' || c == 'E' ||
		(c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isDigit reports a decimal digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
