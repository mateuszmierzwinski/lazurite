//go:build linux

package tty

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
)

// pty is a pseudo-terminal: the test holds the master, the adapter is
// given the other end, exactly as a terminal emulator and a program are.
type pty struct {
	master, slave *os.File
	mu            sync.Mutex
	seen          []byte
}

// openPty makes a pseudo-terminal, or skips the test where there are none.
func openPty(t *testing.T) *pty {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	var number uint32
	var unlock int32
	if ioctl(master.Fd(), syscall.TIOCGPTN, unsafe.Pointer(&number)) != nil || ioctl(master.Fd(), syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)) != nil {
		t.Skip("the pseudo-terminal could not be set up")
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(number)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("the pseudo-terminal could not be opened: %v", err)
	}
	p := &pty{master: master, slave: slave}
	ws := winsize{rows: 30, cols: 100}
	_ = ioctl(master.Fd(), syscall.TIOCSWINSZ, unsafe.Pointer(&ws))
	go p.collect()
	t.Cleanup(func() { slave.Close(); master.Close() })
	return p
}

// collect keeps what the program wrote to the terminal.
func (p *pty) collect() {
	buf := make([]byte, 4096)
	for {
		n, err := syscall.Read(int(p.master.Fd()), buf)
		if n > 0 {
			p.mu.Lock()
			p.seen = append(p.seen, buf[:n]...)
			p.mu.Unlock()
		}
		if err != nil || n <= 0 {
			return
		}
	}
}

// shows waits for the terminal to have been written a text.
func (p *pty) shows(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		got := string(p.seen)
		p.mu.Unlock()
		if strings.Contains(got, want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the terminal was never written %q", want)
}

// types sends keys as a person at the terminal would.
func (p *pty) types(t *testing.T, keys string) {
	t.Helper()
	if _, err := syscall.Write(int(p.master.Fd()), []byte(keys)); err != nil {
		t.Fatal(err)
	}
}

// flags reads the terminal's local flags.
func (p *pty) flags(t *testing.T) uint32 {
	t.Helper()
	var st syscall.Termios
	if err := ioctl(p.slave.Fd(), syscall.TCGETS, unsafe.Pointer(&st)); err != nil {
		t.Fatal(err)
	}
	return st.Lflag
}

// next waits for the next event.
func next(t *testing.T, terminal ide.Terminal) term.Event {
	t.Helper()
	select {
	case ev, ok := <-terminal.Events():
		if !ok {
			t.Fatal("the terminal's events ended")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event arrived")
	}
	return term.Event{}
}

func TestSomethingThatIsNotATerminalIsRefused(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if _, _, err := Open(r, w); err != ErrNotATerminal {
		t.Fatalf("a pipe answered %v", err)
	}
}

func TestATerminalIsTakenOverReadDrawnOnAndGivenBack(t *testing.T) {
	p := openPty(t)
	cooked := p.flags(t)
	terminal, restore, err := Open(p.slave, p.slave)
	if err != nil {
		t.Fatal(err)
	}
	p.shows(t, "\x1b[?1049h")
	if p.flags(t)&(syscall.ICANON|syscall.ECHO|syscall.ISIG) != 0 {
		t.Fatal("the terminal is not in raw mode")
	}
	if w, h := terminal.Size(); w != 100 || h != 30 {
		t.Fatalf("size %dx%d", w, h)
	}
	p.types(t, "a\x1b[A\x03")
	if ev := next(t, terminal); ev.Key != term.KeyRune || ev.Rune != 'a' {
		t.Fatalf("got %+v", ev)
	}
	if ev := next(t, terminal); ev.Key != term.KeyUp {
		t.Fatalf("got %+v", ev)
	}
	if ev := next(t, terminal); ev.Rune != 'c' || ev.Mods != term.Ctrl {
		t.Fatalf("Ctrl+C is a key here, not a signal: %+v", ev)
	}
	p.types(t, "\x1b")
	if ev := next(t, terminal); ev.Key != term.KeyEsc {
		t.Fatalf("a lone Escape was read as %+v", ev)
	}
	if err := terminal.Show([]byte("FRAME")); err != nil {
		t.Fatal(err)
	}
	p.shows(t, "FRAME")
	restore()
	p.shows(t, "\x1b[?1049l")
	if p.flags(t) != cooked {
		t.Fatal("the terminal was not given back as it was found")
	}
	if _, ok := <-terminal.Events(); ok {
		t.Fatal("events went on after the terminal was given back")
	}
}

func TestAResizeIsReportedAsAnEvent(t *testing.T) {
	p := openPty(t)
	terminal, restore, err := Open(p.slave, p.slave)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	time.Sleep(50 * time.Millisecond)
	ws := winsize{rows: 40, cols: 120}
	if err := ioctl(p.master.Fd(), syscall.TIOCSWINSZ, unsafe.Pointer(&ws)); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
	if ev := next(t, terminal); ev.Kind != term.EvResize || ev.X != 120 || ev.Y != 40 {
		t.Fatalf("got %+v", ev)
	}
}

func TestALentTerminalIsCookedAndItsKeysAreNotTaken(t *testing.T) {
	p := openPty(t)
	terminal, restore, err := Open(p.slave, p.slave)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	err = terminal.Lend(func() {
		if p.flags(t)&syscall.ICANON == 0 {
			t.Error("a lent terminal is still raw")
		}
		p.shows(t, "\x1b[?1049l")
		p.types(t, "for the program\n")
		line := make([]byte, 64)
		n, _ := syscall.Read(int(p.slave.Fd()), line)
		if string(line[:n]) != "for the program\n" {
			t.Errorf("the program read %q — the IDE took its keys", line[:n])
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.flags(t)&syscall.ICANON != 0 {
		t.Fatal("the terminal was not taken back raw")
	}
	p.types(t, "z")
	if ev := next(t, terminal); ev.Rune != 'z' {
		t.Fatalf("after the terminal came back the first key was %+v", ev)
	}
}

func TestATerminalThatWillNotSayItsSizeIsTakenAsEightyByTwentyFour(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if cols, rows := (&Terminal{in: r, out: w}).Size(); cols != 80 || rows != 24 {
		t.Fatalf("got %dx%d", cols, rows)
	}
	if ready(r.Fd(), 1) {
		t.Fatal("an empty pipe is ready to read")
	}
}

func TestWhenTheOutputIsAPipeTheSizeIsAskedOfTheInput(t *testing.T) {
	p := openPty(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	terminal, restore, err := Open(p.slave, w)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	if cols, rows := terminal.Size(); cols != 100 || rows != 30 {
		t.Fatalf("with a pipe for output the size was taken as %dx%d", cols, rows)
	}
}

func TestTheScreenIsDrawnOnTheTerminalEvenWhenStandardOutputIsAPipe(t *testing.T) {
	p := openPty(t)
	if out, done := Output(p.slave); out != p.slave {
		done()
		t.Fatal("a terminal was not used as it is")
	}
	if IsTerminal(p.master) == false && IsTerminal(p.slave) == false {
		t.Fatal("a terminal is not known as one")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if IsTerminal(w) {
		t.Fatal("a pipe is taken for a terminal")
	}
	out, done := Output(w)
	defer done()
	if out != w && !IsTerminal(out) {
		t.Fatal("what was opened in place of the pipe is not a terminal")
	}
}
