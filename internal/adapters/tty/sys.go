//go:build linux

package tty

import (
	"syscall"
	"time"
	"unsafe"
)

// winsize is what the kernel says a terminal's size is.
type winsize struct {
	rows, cols, xpixel, ypixel uint16
}

// pollFd is one descriptor asked about by poll(2).
type pollFd struct {
	fd      int32
	events  int16
	revents int16
}

// pollIn is poll's "there is something to read".
const pollIn = 0x1

// ioctl makes one ioctl call.
func ioctl(fd uintptr, request uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

// ready waits up to some milliseconds for a descriptor to have something
// to read, and reports whether it has.
func ready(fd uintptr, millis int) bool {
	p := pollFd{fd: int32(fd), events: pollIn}
	ts := syscall.Timespec{Nsec: int64(millis) * int64(time.Millisecond)}
	n, _, errno := syscall.Syscall6(syscall.SYS_PPOLL, uintptr(unsafe.Pointer(&p)), 1, uintptr(unsafe.Pointer(&ts)), 0, 0, 0)
	return errno == 0 && n > 0 && p.revents&pollIn != 0
}

// idle waits a moment: what the reader does while the terminal is lent.
func idle() {
	time.Sleep(pollMillis * time.Millisecond)
}
