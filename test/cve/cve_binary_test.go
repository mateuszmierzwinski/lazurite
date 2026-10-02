//go:build linux

package cve_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/test/stack"
)

// run starts the real binary on a pseudo-terminal.
func run(t *testing.T, dir string, args ...string) (*stack.Pty, *exec.Cmd, chan error) {
	t.Helper()
	pty := stack.OpenPty(t, 100, 30)
	cmd := exec.Command(stack.Binary(t), args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Dir = pty.Slave, pty.Slave, pty.Slave, dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return pty, cmd, ended
}

// TestTheLogIsPrivateAndHoldsNothingThatWasTyped — a diagnostics file is
// where editors have leaked what was being edited.
func TestTheLogIsPrivateAndHoldsNothingThatWasTyped(t *testing.T) {
	dir := t.TempDir()
	stack.Put(t, filepath.Join(dir, "a.txt"), "token = s3cr3t-in-the-file\n")
	logPath := filepath.Join(dir, "ide.log")
	pty, _, ended := run(t, dir, "-gopls", "", "-log", logPath, filepath.Join(dir, "a.txt"), filepath.Join(dir, "missing-dir", "b.txt"))
	if !pty.Shows("b.txt", 20*time.Second) {
		t.Fatalf("the IDE did not start:\n%q", pty.Written())
	}
	pty.Type("typed-s3cr3t")
	pty.Type("\x1bx")
	if !pty.Shows("Save changes", 10*time.Second) {
		t.Fatal("the IDE did not ask before leaving")
	}
	pty.Type("n")
	pty.Type("n")
	select {
	case <-ended:
	case <-time.After(20 * time.Second):
		t.Fatal("the IDE did not exit")
	}
	info, err := os.Stat(logPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the log is not private: %v %v", info, err)
	}
	logged, _ := os.ReadFile(logPath)
	if strings.Contains(string(logged), "s3cr3t") {
		t.Fatalf("the log holds what was in the file or typed: %s", logged)
	}
}

// TestAKilledIDEStillGivesTheTerminalBack — a terminal left in raw mode on
// the alternate screen is a shell the user cannot use.
func TestAKilledIDEStillGivesTheTerminalBack(t *testing.T) {
	dir := t.TempDir()
	pty, cmd, ended := run(t, dir, "-gopls", "")
	if !pty.Shows("\x1b[?1049h", 20*time.Second) || !pty.Shows("ebug", 20*time.Second) {
		t.Fatal("the IDE did not start")
	}
	_ = cmd.Process.Signal(os.Interrupt)
	time.Sleep(300 * time.Millisecond)
	select {
	case <-ended:
		t.Fatal("an interrupt meant for a program the IDE runs ended the IDE")
	default:
	}
	_ = cmd.Process.Signal(syscallTerm)
	select {
	case <-ended:
	case <-time.After(20 * time.Second):
		t.Fatal("the IDE did not end when asked to")
	}
	if !pty.Shows("\x1b[?1049l", 5*time.Second) {
		t.Fatal("the terminal was left on the alternate screen")
	}
}

// TestHostileArgumentsAreFilesNotOptionsOrCommands — a name on the command
// line is a file to open, whatever it looks like.
func TestHostileArgumentsAreFilesNotOptionsOrCommands(t *testing.T) {
	dir := t.TempDir()
	planted := filepath.Join(dir, "planted")
	pty, _, ended := run(t, dir, "-gopls", "", "--", "$(touch "+planted+")", "; touch "+planted, "-no-debugger")
	if !pty.Shows("F10", 20*time.Second) {
		t.Fatalf("the IDE did not start:\n%q", pty.Written())
	}
	pty.Type("\x1bx")
	select {
	case <-ended:
	case <-time.After(20 * time.Second):
		t.Fatal("the IDE did not exit")
	}
	if _, err := os.Stat(planted); err == nil {
		t.Fatal("an argument was run as a command")
	}
}
