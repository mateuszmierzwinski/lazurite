// Package gotool runs the Go tool: caught, for builds and tests whose
// output the IDE shows; or on the terminal itself, for running the user's
// program.
package gotool

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
)

// Tool is the build tools on this machine: the Go tool, and Bazel.
type Tool struct {
	// command is the Go tool's name or path; bazel is Bazel's.
	command, bazel string
	// in and out are the terminal a foreground job is run on.
	in       io.Reader
	out, err io.Writer
}

// New is the build tools as the IDE's port. command is the Go tool's name
// or path and bazel is Bazel's; in and out are what a program run in the
// foreground reads and writes.
func New(command, bazel string, in io.Reader, out, err io.Writer) ide.Runner {
	return &Tool{command: command, bazel: bazel, in: in, out: out, err: err}
}

// program is the tool a job is run with.
func (t *Tool) program(job ide.Job) string {
	if job.Tool == ide.ToolBazel {
		return t.bazel
	}
	return t.command
}

// maxLine bounds one line of output: a program that never ends a line must
// not grow a buffer without end.
const maxLine = 16 << 10

// killWait is how long a stopped job's pipes are waited on before they are
// closed under it.
const killWait = 2 * time.Second

// Capture runs a job and hands over what it prints, line by line, then how
// it ended.
//
// It starts one goroutine, owned by the job: it reads the output until the
// job closes it, waits for the job, calls done, and ends. The function
// returned stops the job — and everything the job started — which ends the
// goroutine the same way.
func (t *Tool) Capture(ctx context.Context, job ide.Job, line func(string), done func(error)) func() {
	ctx, cancel := context.WithCancel(ctx)
	cmd := t.caught(ctx, job)
	reader, writer := io.Pipe()
	cmd.Stdout, cmd.Stderr = writer, writer
	if err := cmd.Start(); err != nil {
		cancel()
		go done(err)
		return nothingToStop
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		lines(reader, line)
	}()
	go func() {
		err := cmd.Wait()
		writer.Close()
		wg.Wait()
		cancel()
		done(err)
	}()
	return cancel
}

// nothingToStop is what stops a job that never started.
func nothingToStop() {
	// No process was made.
}

// caught is the command of a job whose output is caught. It is started in
// a process group of its own, and stopping it kills the group: `go test`
// starts the tests as another process, which killing `go` alone would
// leave running.
func (t *Tool) caught(ctx context.Context, job ide.Job) *exec.Cmd {
	cmd := exec.CommandContext(ctx, t.program(job), job.Args...)
	cmd.Dir = job.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = killWait
	return cmd
}

// lines reads a stream to its end and hands each line over, an overlong
// one in pieces.
func lines(r io.Reader, line func(string)) {
	buffered := bufio.NewReaderSize(r, maxLine)
	for {
		chunk, _, err := buffered.ReadLine()
		if len(chunk) > 0 || err == nil {
			line(string(chunk))
		}
		if err != nil {
			return
		}
	}
}

// Foreground runs a job on the terminal itself and returns when the user
// has pressed Enter after it ended. A job with no arguments only waits for
// that Enter: the user is looking at the screen as the last program left
// it.
func (t *Tool) Foreground(ctx context.Context, job ide.Job) error {
	if len(job.Args) == 0 {
		t.pause("Press Enter to return to " + ide.Brand)
		return nil
	}
	cmd := exec.CommandContext(ctx, t.program(job), job.Args...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = job.Dir, t.in, t.out, t.err
	err := cmd.Run()
	if err != nil {
		fmt.Fprintf(t.out, "\n[%v]\n", err)
	}
	t.pause("Program ended — press Enter to return to " + ide.Brand)
	if _, exited := err.(*exec.ExitError); exited {
		return nil
	}
	return err
}

// pause shows a line and waits for Enter.
func (t *Tool) pause(prompt string) {
	fmt.Fprintf(t.out, "\n%s ", prompt)
	one := make([]byte, 1)
	for {
		n, err := t.in.Read(one)
		if err != nil || (n == 1 && (one[0] == '\n' || one[0] == '\r')) {
			return
		}
	}
}

// Available reports whether the tool can be found.
func Available(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

// Stdio is the terminal a foreground job runs on.
func Stdio() (io.Reader, io.Writer, io.Writer) { return os.Stdin, os.Stdout, os.Stderr }
