// Package framing is the base protocol LSP and DAP share: a JSON body behind
// Content-Length headers, over a stream.
//
// It is one package rather than a copy in each adapter because the two
// protocols use the SAME framing — the Debug Adapter Protocol's specification
// says so outright — and a second implementation would be a second place for
// the bounds below to be got wrong.
//
// It knows nothing of either protocol: no method names, no lifecycle, no JSON
// beyond "the body is bytes".
package framing

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The bounds on an incoming message.
//
// These are the security surface of this package. A language server is a
// subprocess, and a broken or hostile one announcing a four-gigabyte body must
// cost this process NOTHING — which means the announced length is compared
// before it is passed to make, never after.
//
// The content cap is generous rather than tight: a semantic-token response over
// a large file, or a workspace-wide rename edit, are legitimately megabytes.
// Sixteen is far above anything gopls sends and far below anything that hurts.
const (
	// maxContentLength is the largest message body this client will read.
	maxContentLength = 16 << 20
	// maxHeaderBytes bounds the whole header block, so a server that sends
	// header bytes and never a blank line is refused rather than buffered.
	maxHeaderBytes = 8 << 10
	// maxHeaderLines bounds how many headers one message may carry. The
	// protocol defines two.
	maxHeaderLines = 32
)

// ReadFrame reads one Content-Length framed message body.
//
// It returns the body with the headers stripped. Every error it returns is
// fatal to the stream: see errors.go for why a bad frame can never be skipped.
func ReadFrame(r *bufio.Reader) ([]byte, error) {
	headers, err := readHeaders(r)
	if err != nil {
		return nil, err
	}
	size, err := contentLengthOf(headers, maxContentLength)
	if err != nil {
		return nil, err
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// readHeaders reads the header block up to its blank line.
//
// It refuses a repeated header rather than resolving it: two Content-Length
// values are the message-smuggling class, and whichever one this client
// believes, another reader of the same bytes may believe the other.
func readHeaders(r *bufio.Reader) (map[string]string, error) {
	headers := make(map[string]string, 2)
	budget := maxHeaderBytes
	for count := 0; ; count++ {
		line, err := readHeaderLine(r, budget)
		if err != nil {
			return nil, err
		}
		budget -= len(line) + 2
		if line == "" {
			return headers, nil
		}
		if count >= maxHeaderLines || budget <= 0 {
			return nil, ErrHeaderTooLarge
		}
		if err := addHeader(headers, line); err != nil {
			return nil, err
		}
	}
}

// addHeader parses one line into the set, refusing a repeat.
func addHeader(headers map[string]string, line string) error {
	name, value, err := parseHeaderLine(line)
	if err != nil {
		return err
	}
	if _, already := headers[name]; already {
		return fmt.Errorf("%w: %s", ErrDuplicateHeader, name)
	}
	headers[name] = value
	return nil
}

// readHeaderLine reads one CRLF-terminated line, bounded.
//
// Bounded by reading a byte at a time against a budget rather than by
// bufio.ReadString, which grows its buffer to whatever it is fed: a server
// sending a gigabyte with no newline in it would be answered with a gigabyte of
// allocation before anything got the chance to refuse it.
func readHeaderLine(r *bufio.Reader, budget int) (string, error) {
	var line strings.Builder
	for line.Len() <= budget {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '\n' {
			return strings.TrimSuffix(line.String(), "\r"), nil
		}
		line.WriteByte(b)
	}
	return "", ErrHeaderTooLarge
}

// WriteFrame writes one message with its Content-Length header.
//
// One Write call rather than a header write followed by a body write: the two
// go down a pipe another process is reading, and a reader that saw the header
// and then waited would be waiting on our scheduler rather than on the message.
func WriteFrame(w io.Writer, body []byte) error {
	frame := make([]byte, 0, len(body)+48)
	frame = append(frame, "Content-Length: "...)
	frame = appendInt(frame, len(body))
	frame = append(frame, "\r\n\r\n"...)
	frame = append(frame, body...)
	n, err := w.Write(frame)
	if err != nil {
		return err
	}
	if n != len(frame) {
		return errors.New("lsp: short write to the server")
	}
	return nil
}

// contentLengthOf reads the body size out of a parsed header set.
//
// The whole point of this function is that it refuses BEFORE anything is
// allocated: a server announcing four gigabytes is answered with an error, not
// with a four-gigabyte make. Absent, unparseable, negative and oversized are
// four distinct refusals because a log that says which one is a log that names
// the broken server's actual fault.
func contentLengthOf(headers map[string]string, max int) (int, error) {
	raw, ok := headers["content-length"]
	if !ok {
		return 0, ErrNoContentLength
	}
	size, err := strconv.Atoi(raw)
	if err != nil {
		return 0, ErrBadContentLength
	}
	if size < 0 {
		return 0, ErrBadContentLength
	}
	if size > max {
		return 0, ErrMessageTooLarge
	}
	return size, nil
}

// parseHeaderLine splits one header line into its lower-cased name and its
// value.
//
// Lower-cased because the names are case-insensitive and a server writing
// `content-length` is not wrong. Whitespace around the value is dropped; a line
// with no colon in it is refused rather than guessed at.
func parseHeaderLine(line string) (name, value string, err error) {
	name, value, found := strings.Cut(line, ":")
	if !found || strings.TrimSpace(name) == "" {
		return "", "", ErrMalformedHeader
	}
	return strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(value), nil
}

// The framing faults, named so a caller can tell a malformed stream from a
// protocol error carried inside a well-formed one.
var (
	// ErrMessageTooLarge is a Content-Length past the cap.
	//
	// Refused BEFORE anything is allocated. A server announcing four gigabytes
	// must cost this process nothing at all, which means the number is compared
	// before it is ever passed to make.
	ErrMessageTooLarge = errors.New("lsp: message is larger than the cap")
	// ErrMalformedHeader is a header line with no colon in it.
	ErrMalformedHeader = errors.New("lsp: header line is malformed")
	// ErrNoContentLength is a message whose headers did not say how long it is.
	ErrNoContentLength = errors.New("lsp: message has no Content-Length header")
	// ErrBadContentLength is a length that is not a non-negative integer.
	ErrBadContentLength = errors.New("lsp: Content-Length is not a valid length")
	// ErrHeaderTooLarge is a header block that never ended, or one line of it
	// that ran on. A server that sends header bytes and no blank line would
	// otherwise buffer without limit.
	ErrHeaderTooLarge = errors.New("lsp: header block is larger than the cap")
	// ErrDuplicateHeader is the same header twice.
	//
	// Two Content-Length values are request smuggling, exactly as they are in
	// HTTP: whichever one this client believes, a second reader of the same
	// stream may believe the other. It is refused rather than resolved.
	ErrDuplicateHeader = errors.New("lsp: message repeats a header")
)

// appendInt writes a decimal integer onto a byte slice without allocating a
// string for it. The frame builder is on the write path of every keystroke's
// didChange, which is the one place in this package that runs at typing speed.
func appendInt(dst []byte, n int) []byte { return strconv.AppendInt(dst, int64(n), 10) }
