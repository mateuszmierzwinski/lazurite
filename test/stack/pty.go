//go:build linux

package stack

import (
	"bytes"
	"os"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Pty is a pseudo-terminal a test holds the far end of: what the real
// binary is run on.
type Pty struct {
	Master, Slave *os.File

	mu   sync.Mutex
	seen []byte
}

// winsize is a terminal's size as the kernel keeps it.
type winsize struct {
	rows, cols, xpixel, ypixel uint16
}

// control makes one ioctl call.
func control(f *os.File, request uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), request, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

// OpenPty makes a pseudo-terminal of a size, or skips the test on a
// machine that has none.
func OpenPty(t *testing.T, cols, rows uint16) *Pty {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	var number uint32
	var unlock int32
	if control(master, syscall.TIOCGPTN, unsafe.Pointer(&number)) != nil || control(master, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)) != nil {
		t.Skip("the pseudo-terminal could not be set up")
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(number)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("the pseudo-terminal could not be opened: %v", err)
	}
	_ = control(master, syscall.TIOCSWINSZ, unsafe.Pointer(&winsize{rows: rows, cols: cols}))
	p := &Pty{Master: master, Slave: slave}
	go p.collect(int(master.Fd()))
	t.Cleanup(func() { _ = slave.Close(); _ = master.Close() })
	return p
}

// collect keeps what is written to the terminal. It ends when the
// terminal is closed. The descriptor is taken once, by the caller: asking the
// file for it on every read would race with the Close that ends this loop.
func (p *Pty) collect(fd int) {
	buf := make([]byte, 8192)
	for {
		n, err := syscall.Read(fd, buf)
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

// Written is everything written to the terminal so far.
func (p *Pty) Written() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.seen...)
}

// Shows waits for the terminal to have been written a text.
func (p *Pty) Shows(want string, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if bytes.Contains(p.Written(), []byte(want)) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Type sends keys as a person at the terminal would.
func (p *Pty) Type(keys string) {
	_, _ = syscall.Write(int(p.Master.Fd()), []byte(keys))
}
