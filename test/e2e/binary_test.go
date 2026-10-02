//go:build linux

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/test/stack"
)

func TestTheBinarySaysItsVersionAndItsUsage(t *testing.T) {
	out, err := exec.Command(stack.Binary(t), "-version").CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), "Lazurite ") {
		t.Fatalf("got %q, %v", out, err)
	}
	out, _ = exec.Command(stack.Binary(t), "-help").CombinedOutput()
	for _, want := range []string{"Usage: lazurite [options] [directory | file...]", "a console IDE for Go", "-gopls", "-no-debugger", "-log"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("the usage does not say %q:\n%s", want, out)
		}
	}
}

func TestTheBinaryRefusesToRunWithoutATerminal(t *testing.T) {
	cmd := exec.Command(stack.Binary(t))
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "standard input is not a terminal") {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestTheBinaryDrawsItsDesktopOnARealTerminalAndGivesItBackOnExit(t *testing.T) {
	pty := stack.OpenPty(t, 100, 30)
	dir := t.TempDir()
	stack.Put(t, filepath.Join(dir, "hello.go"), "package main\n\nfunc main() {}\n")
	cmd := exec.Command(stack.Binary(t), "-gopls", "", "-log", filepath.Join(dir, "lazurite.log"), filepath.Join(dir, "hello.go"))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = pty.Slave, pty.Slave, pty.Slave
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	defer cmd.Process.Kill()
	for _, want := range []string{"\x1b[?1049h", "ile", "ebug", "hello.go", "func", "F10"} {
		if !pty.Shows(want, 20*time.Second) {
			t.Fatalf("the terminal was never written %q:\n%q", want, pty.Written())
		}
	}
	pty.Type("x")
	pty.Type("\x1bx")
	if !pty.Shows("Save changes to hello.go?", 10*time.Second) {
		t.Fatalf("leaving with unsaved edits did not ask:\n%q", pty.Written())
	}
	pty.Type("n")
	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("the binary ended with %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the binary did not exit")
	}
	if !pty.Shows("\x1b[?1049l", 5*time.Second) {
		t.Fatal("the terminal was not given back")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "hello.go")); string(got) != "package main\n\nfunc main() {}\n" {
		t.Fatalf("answering No wrote the file: %q", got)
	}
}

// TestStartedWithItsOutputInAPipeTheBinaryStillFillsTheTerminal — a
// launcher that keeps a copy of what a program prints (Basalt does) gives
// it a pipe for standard output. The IDE used to draw into the pipe and,
// unable to ask a pipe how big the terminal is, drew 80 columns by 24.
func TestStartedWithItsOutputInAPipeTheBinaryStillFillsTheTerminal(t *testing.T) {
	pty := stack.OpenPty(t, 132, 43)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	dir := t.TempDir()
	stack.Put(t, filepath.Join(dir, "hello.go"), "package main\n\nfunc main() {}\n")
	cmd := exec.Command(stack.Binary(t), "-gopls", "", "hello.go")
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Dir = pty.Slave, writer, writer, dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	defer cmd.Process.Kill()
	if !pty.Shows("\x1b[43;1H", 20*time.Second) || !pty.Shows("hello.go", 5*time.Second) {
		t.Fatalf("the last row of a 43-row terminal was never drawn:\n%q", pty.Written())
	}
	if strings.Contains(string(pty.Written()), "\x1b[25;1H") == false {
		t.Fatal("the rows beyond the 24th were not drawn")
	}
	pty.Type("\x1bx")
	select {
	case <-ended:
	case <-time.After(20 * time.Second):
		t.Fatal("the binary did not exit")
	}
	piped := make([]byte, 4096)
	n, _ := reader.Read(piped)
	if strings.Contains(string(piped[:n]), "\x1b[") {
		t.Fatalf("the screen was drawn into the pipe: %q", piped[:n])
	}
}

// TestUnderBazelRunTheProjectIsWhereTheUserWas — `bazel run` starts a
// program among Bazel's own files and says where the user was in
// BUILD_WORKING_DIRECTORY.
func TestUnderBazelRunTheProjectIsWhereTheUserWas(t *testing.T) {
	pty := stack.OpenPty(t, 100, 30)
	project, elsewhere := t.TempDir(), t.TempDir()
	stack.Put(t, filepath.Join(project, "go.mod"), "module where\n\ngo 1.24\n")
	stack.Put(t, filepath.Join(project, "cmd", "tool", "found_me.go"), "package main\n\nfunc main() {}\n")
	cmd := exec.Command(stack.Binary(t), "-gopls", "", "cmd/tool/found_me.go")
	cmd.Stdin, cmd.Stdout, cmd.Stderr, cmd.Dir = pty.Slave, pty.Slave, pty.Slave, elsewhere
	cmd.Env = append(os.Environ(), "BUILD_WORKING_DIRECTORY="+project)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	defer cmd.Process.Kill()
	for _, want := range []string{" cmd/tool/found_me.go ", "go.mod", filepath.Base(project)} {
		if !pty.Shows(want, 20*time.Second) {
			t.Fatalf("the terminal was never written %q:\n%q", want, pty.Written())
		}
	}
	pty.Type("\x1bx")
	select {
	case <-ended:
	case <-time.After(20 * time.Second):
		t.Fatal("the binary did not exit")
	}
}
