package delve

import (
	"bufio"
	"io"
	"os"
	"syscall"

	"github.com/go-delve/delve/pkg/proc"
	"github.com/go-delve/delve/service/debugger"
)

// How a client differs from the default one.
//
// Both options are INERT unless asked for: a client built with none behaves
// exactly as it did before they existed, which is what the window that was
// here first (lazurite) relies on.

// Option changes how a client launches a program.
type Option func(*Client)

// Held makes Launch leave the program stopped at its entry rather than
// letting it go.
//
// The default lets the program run the moment it is launched, and a caller
// that sets its breakpoints afterwards is racing the program to them. A
// caller that asks for this sets them in between and calls Continue itself,
// which is the order the protocol means.
func Held() Option { return func(c *Client) { c.waits = true } }

// Captured sends what the program writes to the watcher's Output instead of
// to this process's own terminal, and gives the program no input.
//
// A window draws elsewhere and does not care where a debuggee writes. A
// console editor does: the program would write over the screen the editor is
// drawing, and read the keys the editor is waiting for.
func Captured() Option { return func(c *Client) { c.captured = true } }

// noInput is what a captured program reads from.
const noInput = "/dev/null"

// redirect points a session's output at the watcher when the client was
// built Captured, and leaves the configuration alone otherwise.
func (c *Client) redirect(config *debugger.Config) {
	if !c.captured {
		return
	}
	config.Stdin = noInput
	config.Stdout = c.pipe()
	config.Stderr = c.pipe()
}

// pipe makes one stream of the program's output flow to the watcher. A
// stream that cannot be made is left where it was: a program that can be
// debugged with its output in the wrong place is better than one that
// cannot be debugged.
func (c *Client) pipe() proc.OutputRedirect {
	reader, redirect, err := proc.Redirector()
	if err != nil {
		return proc.OutputRedirect{}
	}
	c.streams = append(c.streams, redirect.Path)
	go c.forward(reader)
	return redirect
}

// maxOutputLine bounds one line of a program's output: a program that never
// writes a newline must not grow a buffer without end.
const maxOutputLine = 64 << 10

// forward reads one stream until the program closes it and hands each line
// to the watcher.
//
// It runs on a goroutine of its own, owned by the session, and it is the ONLY
// thing that ever touches the reader: Delve's reader opens its pipe lazily on
// the first Read and is not safe to close from another goroutine. It ends when
// the stream does — when the program exits, or when closeStreams hangs the
// pipe up from the writing side, whichever is first.
func (c *Client) forward(reader io.ReadCloser) {
	defer func() { _ = reader.Close() }()
	lines := bufio.NewReaderSize(reader, maxOutputLine)
	for {
		line, err := lines.ReadString('\n')
		if line != "" && c.watcher != nil {
			c.watcher.Output(line)
		}
		if err != nil {
			return
		}
	}
}

// closeStreams ends the forwarding of a session that is over, by hanging up
// each pipe rather than by closing a reader another goroutine is using. Called
// with the lock held.
func (c *Client) closeStreams() {
	for _, path := range c.streams {
		hangUp(path)
	}
	c.streams = nil
}

// hangUp opens a pipe for writing and closes it again, which gives its reader
// an end of file — also a reader still waiting in open for a program that
// never got as far as writing. A pipe that is already gone (its forwarder
// finished and removed it) has nobody left to tell.
func hangUp(path string) {
	if path == "" {
		return
	}
	writer, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0) // #nosec G304 -- the session's own pipe, named by Delve's Redirector
	if err != nil {
		return
	}
	_ = writer.Close()
}
