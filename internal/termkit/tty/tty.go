//go:build linux

// Package tty is the real terminal: raw mode, the alternate screen, the
// keys read from it and the frames written to it.
//
// It uses nothing but the standard library: the terminal is set up with
// two ioctls and read with poll, so the binary stays pure Go and static.
package tty

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"

	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

// Terminal is a terminal taken over.
//
// mu protects lent, saved and raw: the reader holds it for each read, and
// Lend takes it to make sure nothing is reading while the terminal belongs
// to something else.
type Terminal struct {
	in, out *os.File
	events  chan term.Event

	mu    sync.Mutex
	saved syscall.Termios
	lent  bool

	done chan struct{}
	wg   sync.WaitGroup
}

// Open takes the terminal over: raw mode, the alternate screen, mouse
// reporting. Close gives it back. It returns the port and the function
// that gives the terminal back, which must be called before the process
// ends or the user's shell is left in raw mode.
func Open(in, out *os.File) (Port, func(), error) {
	t := &Terminal{
		in: in, out: out,
		// 64: a paste or a burst of mouse movement arrives faster than a
		// frame is drawn; the reader must not stall on it.
		events: make(chan term.Event, 64),
		done:   make(chan struct{}),
	}
	if err := t.enter(); err != nil {
		return nil, nil, err
	}
	t.wg.Add(2)
	go t.read()
	go t.watchSize()
	return t, t.close, nil
}

// enter saves the terminal's settings and switches it to raw mode and the
// alternate screen.
func (t *Terminal) enter() error {
	if err := ioctl(t.in.Fd(), syscall.TCGETS, unsafe.Pointer(&t.saved)); err != nil {
		return ErrNotATerminal
	}
	if err := t.raw(); err != nil {
		return err
	}
	_, err := t.out.WriteString(term.Enter)
	return err
}

// raw switches the terminal to raw mode: keys arrive as typed, unechoed,
// and Ctrl+C, Ctrl+Z, Ctrl+S and Ctrl+Q are keys rather than signals.
func (t *Terminal) raw() error {
	raw := t.saved
	raw.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON
	raw.Oflag &^= syscall.OPOST
	raw.Cflag |= syscall.CS8
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG
	raw.Cc[syscall.VMIN], raw.Cc[syscall.VTIME] = 1, 0
	return ioctl(t.in.Fd(), syscall.TCSETS, unsafe.Pointer(&raw))
}

// leave gives the terminal back as it was found.
func (t *Terminal) leave() {
	_, _ = t.out.WriteString(term.Leave)
	_ = ioctl(t.in.Fd(), syscall.TCSETS, unsafe.Pointer(&t.saved))
}

// close stops the reader and the size watcher and gives the terminal back.
func (t *Terminal) close() {
	close(t.done)
	t.wg.Wait()
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.lent {
		t.leave()
	}
}

// Events is the keys, mouse, pastes and resizes, in order.
func (t *Terminal) Events() <-chan term.Event { return t.events }

// Size is how many columns and rows the terminal has, asked of the output
// and then of the input — either may be a pipe while the other is the
// terminal; 80×24 when neither will say.
func (t *Terminal) Size() (int, int) {
	for _, f := range []*os.File{t.out, t.in} {
		var ws winsize
		if err := ioctl(f.Fd(), syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err == nil && ws.cols > 0 && ws.rows > 0 {
			return int(ws.cols), int(ws.rows)
		}
	}
	return 80, 24
}

// Output is where the screen should be drawn: standard output when it is
// the terminal, and otherwise the terminal itself.
//
// A program that starts this one may have put a pipe in place of its
// output, to keep a copy of what it prints. Frames drawn into that pipe
// would arrive late and fill the copy with screen-drawing; and the pipe
// cannot say how big the terminal is. The second answer is the function
// that closes what was opened.
func Output(stdout *os.File) (*os.File, func()) {
	if IsTerminal(stdout) {
		return stdout, nothingToClose
	}
	direct, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return stdout, nothingToClose
	}
	return direct, func() { _ = direct.Close() }
}

// nothingToClose is what closes an output this package did not open.
func nothingToClose() {
	// Standard output is the caller's to close.
}

// IsTerminal reports whether a file is a terminal.
func IsTerminal(f *os.File) bool {
	var st syscall.Termios
	return ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&st)) == nil
}

// Show writes a frame.
func (t *Terminal) Show(frame []byte) error {
	_, err := t.out.Write(frame)
	return err
}

// Lend gives the terminal — the user's own screen, cooked input — to
// something else until it returns, then takes it back.
func (t *Terminal) Lend(use func()) error {
	t.mu.Lock()
	t.lent = true
	t.leave()
	t.mu.Unlock()
	use()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lent = false
	if err := t.raw(); err != nil {
		return err
	}
	_, err := t.out.WriteString(term.Enter)
	return err
}

// pollMillis is how long the reader waits for a key before looking at
// whether it should stop, and how long a lone Escape is waited on before
// it is taken to be the Escape key.
const pollMillis = 40

// read is the goroutine that reads the keyboard. It is owned by the
// Terminal: it ends when close is called, and close waits for it.
func (t *Terminal) read() {
	defer t.wg.Done()
	defer close(t.events)
	var pending []byte
	buf := make([]byte, 4096)
	for {
		select {
		case <-t.done:
			return
		default:
		}
		n := t.readSome(buf)
		pending = append(pending, buf[:n]...)
		events, rest := term.Parse(pending, n == 0)
		pending = append(pending[:0], rest...)
		if !t.send(events) {
			return
		}
	}
}

// readSome reads what the keyboard has, waiting a moment for it; none when
// nothing came or the terminal is lent.
func (t *Terminal) readSome(buf []byte) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lent || !ready(t.in.Fd(), pollMillis) {
		if t.lent {
			idle()
		}
		return 0
	}
	n, err := syscall.Read(int(t.in.Fd()), buf)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// send hands events to the loop; false when the terminal was closed
// meanwhile.
func (t *Terminal) send(events []term.Event) bool {
	for _, ev := range events {
		select {
		case t.events <- ev:
		case <-t.done:
			return false
		}
	}
	return true
}

// watchSize is the goroutine that turns the terminal's "my size changed"
// signal into an event. It is owned by the Terminal: it ends when close is
// called, and close waits for it.
func (t *Terminal) watchSize() {
	defer t.wg.Done()
	// 1: signals are not queued; one pending notice is all that is needed.
	changed := make(chan os.Signal, 1)
	signal.Notify(changed, syscall.SIGWINCH)
	defer signal.Stop(changed)
	for {
		select {
		case <-t.done:
			return
		case <-changed:
			w, h := t.Size()
			if !t.send([]term.Event{{Kind: term.EvResize, X: w, Y: h}}) {
				return
			}
		}
	}
}
