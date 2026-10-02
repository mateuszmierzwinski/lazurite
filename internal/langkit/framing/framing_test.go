package framing

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// framed wraps a body in the header the protocol requires.
func framed(body string) string {
	return "Content-Length: " + frameLen(len(body)) + "\r\n\r\n" + body
}

// reading builds a reader over a string, as the session's does over a pipe.
func reading(stream string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(stream))
}

func TestReadFrameReadsOneMessageAndLeavesTheNext(t *testing.T) {
	stream := reading(framed(`{"id":1}`) + framed(`{"id":2}`))
	first, err := ReadFrame(stream)
	if err != nil {
		t.Fatalf("readFrame = %v, want the first message", err)
	}
	if string(first) != `{"id":1}` {
		t.Fatalf("first frame = %q", first)
	}
	second, err := ReadFrame(stream)
	if err != nil {
		t.Fatalf("readFrame = %v, want the second message", err)
	}
	if string(second) != `{"id":2}` {
		t.Fatalf("second frame = %q; the stream lost its place", second)
	}
}

func TestReadFrameToleratesExtraHeadersAndCasing(t *testing.T) {
	stream := reading("content-length: 2\r\nContent-Type: application/vscode-jsonrpc\r\n\r\n{}")
	body, err := ReadFrame(stream)
	if err != nil || string(body) != "{}" {
		t.Fatalf("readFrame = %q, %v; header names are case-insensitive", body, err)
	}
}

func TestReadFrameRefusesAnAnnouncedSizeThatWouldExhaustMemory(t *testing.T) {
	// Four gigabytes, announced by a server that then sends nothing. It must
	// cost this process nothing at all: the number is compared before it is
	// ever passed to make.
	stream := reading("Content-Length: 4294967296\r\n\r\n")
	if _, err := ReadFrame(stream); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("readFrame = %v, want ErrMessageTooLarge", err)
	}
}

func TestReadFrameRefusesAMissingOrUnusableLength(t *testing.T) {
	for name, stream := range map[string]string{
		"absent":     "Content-Type: x\r\n\r\n{}",
		"empty":      "\r\n{}",
		"words":      "Content-Length: lots\r\n\r\n{}",
		"negative":   "Content-Length: -1\r\n\r\n{}",
		"fractional": "Content-Length: 1.5\r\n\r\n{}",
	} {
		if _, err := ReadFrame(reading(stream)); err == nil {
			t.Fatalf("%s: readFrame accepted a message with no usable length", name)
		}
	}
}

func TestReadFrameRefusesARepeatedContentLength(t *testing.T) {
	// Two lengths is the message-smuggling class: whichever this client
	// believes, another reader of the same bytes may believe the other.
	stream := reading("Content-Length: 2\r\nContent-Length: 9\r\n\r\n{}       ")
	if _, err := ReadFrame(stream); !errors.Is(err, ErrDuplicateHeader) {
		t.Fatalf("readFrame = %v, want ErrDuplicateHeader", err)
	}
}

func TestReadFrameRefusesAHeaderBlockThatNeverEnds(t *testing.T) {
	// A server sending header bytes and no blank line must be refused rather
	// than buffered without limit.
	var many strings.Builder
	for i := range 4096 {
		many.WriteString("X-Pad-" + frameLen(i) + ": padding\r\n")
	}
	if _, err := ReadFrame(reading(many.String())); !errors.Is(err, ErrHeaderTooLarge) {
		t.Fatalf("readFrame = %v, want ErrHeaderTooLarge", err)
	}
	noNewline := "Content-Length: " + strings.Repeat("0", maxHeaderBytes*2)
	if _, err := ReadFrame(reading(noNewline)); !errors.Is(err, ErrHeaderTooLarge) {
		t.Fatalf("readFrame on an endless line = %v, want ErrHeaderTooLarge", err)
	}
}

func TestReadFrameRefusesAMalformedHeaderLine(t *testing.T) {
	if _, err := ReadFrame(reading("this is not a header\r\n\r\n{}")); !errors.Is(err, ErrMalformedHeader) {
		t.Fatalf("readFrame = %v, want ErrMalformedHeader", err)
	}
}

func TestReadFrameReportsATruncatedBody(t *testing.T) {
	// A server that announced ten bytes and sent three has gone away mid
	// message; the caller must learn that rather than getting three bytes.
	stream := reading("Content-Length: 10\r\n\r\nabc")
	if _, err := ReadFrame(stream); err == nil {
		t.Fatal("readFrame accepted a body shorter than its announced length")
	}
}

func TestReadFrameAcceptsAnEmptyBody(t *testing.T) {
	body, err := ReadFrame(reading("Content-Length: 0\r\n\r\n"))
	if err != nil || len(body) != 0 {
		t.Fatalf("readFrame = %q, %v; a zero-length message is legal", body, err)
	}
}

func TestReadFrameEndsAtEndOfStream(t *testing.T) {
	if _, err := ReadFrame(reading("")); !errors.Is(err, io.EOF) {
		t.Fatalf("readFrame = %v, want io.EOF; that is how the reader learns the server is gone", err)
	}
}

func TestWriteFrameProducesWhatReadFrameExpects(t *testing.T) {
	var out bytes.Buffer
	if err := WriteFrame(&out, []byte(`{"jsonrpc":"2.0"}`)); err != nil {
		t.Fatalf("writeFrame = %v", err)
	}
	if !strings.HasPrefix(out.String(), "Content-Length: 17\r\n\r\n") {
		t.Fatalf("writeFrame wrote %q", out.String())
	}
	body, err := ReadFrame(reading(out.String()))
	if err != nil || string(body) != `{"jsonrpc":"2.0"}` {
		t.Fatalf("round trip = %q, %v", body, err)
	}
}

// refusingWriter fails every write, the way a pipe to a dead process does.
type refusingWriter struct{}

func (refusingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestWriteFrameReportsAFailedWrite(t *testing.T) {
	if err := WriteFrame(refusingWriter{}, []byte("{}")); err == nil {
		t.Fatal("writeFrame swallowed a broken pipe")
	}
}

// shortWriter accepts only part of what it is given, which is legal for an
// io.Writer and fatal for a framed message.
type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) / 2, nil }

func TestWriteFrameReportsAShortWrite(t *testing.T) {
	if err := WriteFrame(shortWriter{}, []byte("{}")); err == nil {
		t.Fatal("writeFrame accepted half a message onto the wire")
	}
}

func BenchmarkReadFrame(b *testing.B) {
	stream := framed(`{"jsonrpc":"2.0","id":1,"result":{"contents":"a hover answer"}}`)
	for b.Loop() {
		_, _ = ReadFrame(reading(stream))
	}
}

func BenchmarkWriteFrame(b *testing.B) {
	body := []byte(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{}}`)
	for b.Loop() {
		_ = WriteFrame(io.Discard, body)
	}
}

// frameLen keeps the fake free of a strconv import in every helper.
func frameLen(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestParseHeaderLineSplitsAndLowerCases(t *testing.T) {
	name, value, err := parseHeaderLine("Content-Length:  42  ")
	if err != nil || name != "content-length" || value != "42" {
		t.Fatalf("parseHeaderLine = %q, %q, %v", name, value, err)
	}
}
func TestParseHeaderLineRefusesALineThatIsNotOne(t *testing.T) {
	for _, line := range []string{"no colon here", "", ": value with no name", "   : x"} {
		if _, _, err := parseHeaderLine(line); !errors.Is(err, ErrMalformedHeader) {
			t.Fatalf("parseHeaderLine(%q) = %v, want ErrMalformedHeader", line, err)
		}
	}
}
func TestContentLengthOfReadsTheSize(t *testing.T) {
	size, err := contentLengthOf(map[string]string{"content-length": "128"}, 1024)
	if err != nil || size != 128 {
		t.Fatalf("contentLengthOf = %d, %v", size, err)
	}
}
func TestContentLengthOfRefusesEachWayItCanBeWrong(t *testing.T) {
	for name, testCase := range map[string]struct {
		headers map[string]string
		want    error
	}{
		"absent":   {map[string]string{}, ErrNoContentLength},
		"words":    {map[string]string{"content-length": "many"}, ErrBadContentLength},
		"negative": {map[string]string{"content-length": "-1"}, ErrBadContentLength},
		"empty":    {map[string]string{"content-length": ""}, ErrBadContentLength},
		"huge":     {map[string]string{"content-length": "4294967296"}, ErrMessageTooLarge},
	} {
		if _, err := contentLengthOf(testCase.headers, 1024); !errors.Is(err, testCase.want) {
			t.Fatalf("%s: contentLengthOf = %v, want %v", name, err, testCase.want)
		}
	}
}
func TestAppendIntWritesDecimal(t *testing.T) {
	if got := string(appendInt([]byte("n="), 4096)); got != "n=4096" {
		t.Fatalf("appendInt = %q", got)
	}
	if got := string(appendInt(nil, 0)); got != "0" {
		t.Fatalf("appendInt(0) = %q", got)
	}
}
func BenchmarkParseHeaderLine(b *testing.B) {
	for b.Loop() {
		_, _, _ = parseHeaderLine("Content-Length: 4096")
	}
}
func BenchmarkContentLengthOf(b *testing.B) {
	headers := map[string]string{"content-length": "4096"}
	for b.Loop() {
		_, _ = contentLengthOf(headers, maxContentLength)
	}
}
func BenchmarkAppendInt(b *testing.B) {
	buffer := make([]byte, 0, 32)
	for b.Loop() {
		_ = appendInt(buffer[:0], 40960)
	}
}
