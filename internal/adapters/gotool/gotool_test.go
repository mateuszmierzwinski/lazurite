package gotool

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
)

// script writes a shell script that stands in for the Go tool.
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakego")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// caught runs a job and collects what it printed and how it ended.
func caught(t *testing.T, tool ide.Runner, job ide.Job) ([]string, error, func()) {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	ended := make(chan error, 1)
	stop := tool.Capture(context.Background(), job,
		func(line string) { mu.Lock(); lines = append(lines, line); mu.Unlock() },
		func(err error) { ended <- err })
	wait := func() {}
	select {
	case err := <-ended:
		mu.Lock()
		defer mu.Unlock()
		return lines, err, wait
	case <-time.After(300 * time.Millisecond):
	}
	return nil, nil, func() {
		stop()
		select {
		case <-ended:
		case <-time.After(10 * time.Second):
			t.Fatal("a stopped job never ended")
		}
	}
}

func TestAJobsOutputIsCaughtLineByLineWithItsArgumentsAndDirectory(t *testing.T) {
	dir := t.TempDir()
	tool := New(script(t, `echo "args: $*"; pwd; echo "to stderr" >&2`), "", nil, nil, nil)
	lines, err, _ := caught(t, tool, ide.Job{Dir: dir, Args: []string{"build", "./...", "a b"}})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, "|")
	if !strings.Contains(got, "args: build ./... a b") || !strings.Contains(got, filepath.Base(dir)) || !strings.Contains(got, "to stderr") {
		t.Fatalf("got %q", got)
	}
}

func TestAFailingJobSaysHowItEnded(t *testing.T) {
	tool := New(script(t, `echo "main.go:3:1: syntax error"; exit 2`), "", nil, nil, nil)
	lines, err, _ := caught(t, tool, ide.Job{Dir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "exit status 2") || len(lines) != 1 {
		t.Fatalf("got %v, %v", lines, err)
	}
}

func TestAnArgumentIsNeverReadByAShell(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "made")
	tool := New(script(t, `for a in "$@"; do echo "[$a]"; done`), "", nil, nil, nil)
	lines, _, _ := caught(t, tool, ide.Job{Dir: t.TempDir(), Args: []string{"; touch " + marker, "$(touch " + marker + ")", "`touch " + marker + "`"}})
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("an argument was run as a command")
	}
	if len(lines) != 3 || lines[0] != "[; touch "+marker+"]" {
		t.Fatalf("got %q", lines)
	}
}

func TestALineWithoutEndIsHandedOverInPieces(t *testing.T) {
	tool := New(script(t, `head -c 40000 /dev/zero | tr '\0' x; echo; echo end`), "", nil, nil, nil)
	lines, err, _ := caught(t, tool, ide.Job{Dir: t.TempDir()})
	if err != nil || len(lines) < 3 || lines[len(lines)-1] != "end" {
		t.Fatalf("%d lines, %v", len(lines), err)
	}
	for _, line := range lines {
		if len(line) > maxLine {
			t.Fatalf("a line of %d bytes was handed over whole", len(line))
		}
	}
}

func TestStoppingAJobKillsItAndWhatItStarted(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	tool := New(script(t, `sleep 300 & echo $! > `+pidFile+`; wait`), "", nil, nil, nil)
	_, _, stop := caught(t, tool, ide.Job{Dir: t.TempDir()})
	stop()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat("/proc/" + strings.TrimSpace(string(raw)) + "/status"); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the job's own child %s outlived it", strings.TrimSpace(string(raw)))
}

func TestAToolThatCannotBeStartedEndsTheJobWithTheReason(t *testing.T) {
	tool := New(filepath.Join(t.TempDir(), "absent"), "", nil, nil, nil)
	_, err, _ := caught(t, tool, ide.Job{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("a job with no tool ended well")
	}
	if Available(filepath.Join(t.TempDir(), "absent")) || !Available("sh") {
		t.Fatal("Available")
	}
}

func TestAForegroundJobRunsOnTheTerminalAndWaitsForEnter(t *testing.T) {
	var out bytes.Buffer
	tool := New(script(t, `read name; echo "hello $name"; exit 3`), "", strings.NewReader("Ada\nignored\n"), &out, &out)
	if err := tool.Foreground(context.Background(), ide.Job{Dir: t.TempDir(), Args: []string{"run", "."}}); err != nil {
		t.Fatalf("a program that exits with a status is not a failure of the IDE: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "hello Ada") || !strings.Contains(got, "[exit status 3]") || !strings.Contains(got, "press Enter to return to Lazurite") {
		t.Fatalf("got %q", got)
	}
}

func TestAForegroundJobWithNothingToRunOnlyWaits(t *testing.T) {
	var out bytes.Buffer
	tool := New("/nonexistent", "", strings.NewReader("x\r"), &out, &out)
	if err := tool.Foreground(context.Background(), ide.Job{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Press Enter to return to Lazurite") {
		t.Fatalf("got %q", out.String())
	}
	if err := tool.Foreground(context.Background(), ide.Job{Args: []string{"run"}}); err == nil {
		t.Fatal("a tool that cannot be started was reported as run")
	}
	in, o, e := Stdio()
	if in == nil || o == nil || e == nil {
		t.Fatal("Stdio")
	}
}
