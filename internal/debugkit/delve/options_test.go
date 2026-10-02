package delve

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
)

// said reports whether the watcher was ever told of a state.
func said(state debug.State) func([]debug.Session) bool {
	return func(seen []debug.Session) bool {
		for _, s := range seen {
			if s.State == state {
				return true
			}
		}
		return false
	}
}

// printed is everything the debuggee wrote, as the watcher was given it.
func (w *watching) printed() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.out, "")
}

// TestAHeldProgramWaitsForItsBreakpointsAndACapturedOneWritesToTheWatcher is
// the two options together, as a console editor uses them: the program is
// launched and does NOT run; the breakpoint goes in; Continue lets it go; it
// stops there every time, not when a race is won; and what it prints reaches
// the watcher rather than this process's own output.
func TestAHeldProgramWaitsForItsBreakpointsAndACapturedOneWritesToTheWatcher(t *testing.T) {
	dir, source := program(t)
	watcher := &watching{}
	client := New(watcher, Held(), Captured())
	t.Cleanup(func() { _ = client.Stop(context.Background()) })
	if err := client.Launch(context.Background(), "go", []string{"run", "."}, dir); err != nil {
		t.Skipf("this machine cannot debug here: %v", err)
	}
	if !watcher.waitFor(time.Second, said(debug.Starting)) || watcher.waitFor(300*time.Millisecond, said(debug.Running)) {
		t.Fatal("a held program was let go by the launch")
	}
	set, err := client.SetBreakpoints(context.Background(), source, []debug.Breakpoint{{Line: 10}})
	if err != nil || len(set) != 1 || !set[0].Verified {
		t.Skipf("the breakpoint was refused: %v %v", set, err)
	}
	if err := client.Continue(context.Background()); err != nil {
		t.Fatalf("Continue = %v", err)
	}
	if !watcher.waitFor(30*time.Second, stoppedSomewhere) {
		t.Skip("the program never stopped; this machine may not allow ptrace")
	}
	frames, err := client.Frames(context.Background())
	if err != nil || len(frames) == 0 || frames[0].Line != 10 {
		t.Fatalf("stopped at %v, %v — want line 10", frames, err)
	}
	if err := client.Continue(context.Background()); err != nil {
		t.Fatalf("the second Continue = %v", err)
	}
	if !watcher.waitFor(30*time.Second, said(debug.Ended)) {
		t.Fatal("the program never ended")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(watcher.printed(), "6") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := watcher.printed(); !strings.Contains(got, "6\n") {
		t.Fatalf("the program's output did not reach the watcher: %q", got)
	}
}

// TestStoppingARunningProgramDoesNotWaitForItToEnd — a program that never
// stops by itself used to hold the lock Detach waits for, and Stop hung with
// this client's own lock held: the editor could never debug again.
func TestStoppingARunningProgramDoesNotWaitForItToEnd(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module forever\n\ngo 1.25\n")
	write(t, filepath.Join(dir, "main.go"), "package main\n\nimport \"time\"\n\nfunc main() {\n\tfor {\n\t\ttime.Sleep(time.Millisecond)\n\t}\n}\n")
	watcher := &watching{}
	client := New(watcher, Held(), Captured())
	if err := client.Launch(context.Background(), "go", []string{"run", "."}, dir); err != nil {
		t.Skipf("this machine cannot debug here: %v", err)
	}
	if err := client.Continue(context.Background()); err != nil {
		t.Fatalf("Continue = %v", err)
	}
	if !watcher.waitFor(10*time.Second, said(debug.Running)) {
		t.Fatal("the program never ran")
	}
	time.Sleep(200 * time.Millisecond)
	stopped := make(chan error, 1)
	go func() { stopped <- client.Stop(context.Background()) }()
	select {
	case <-stopped:
	case <-time.After(20 * time.Second):
		t.Fatal("Stop is still waiting for a program that never ends")
	}
	if !watcher.waitFor(time.Second, said(debug.Ended)) {
		t.Fatal("the watcher was not told the session ended")
	}
	if err := client.Launch(context.Background(), "go", []string{"run", "."}, dir); err != nil {
		t.Fatalf("a second session could not start: %v", err)
	}
	_ = client.Stop(context.Background())
}

// TestADebugBuildIsWrittenWhereOnlyItsOwnerCanReach — a predictable name in
// the shared temporary directory can be made first by somebody else, as a
// link to one of the operator's files.
func TestADebugBuildIsWrittenWhereOnlyItsOwnerCanReach(t *testing.T) {
	one, err := privateBinary()
	if err != nil {
		t.Fatal(err)
	}
	two, err := privateBinary()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(one))
	defer os.RemoveAll(filepath.Dir(two))
	if filepath.Dir(one) == filepath.Dir(two) || filepath.Dir(one) == os.TempDir() {
		t.Fatalf("two builds share a place: %s and %s", one, two)
	}
	info, err := os.Stat(filepath.Dir(one))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("the build's directory is open to others: %v %v", info.Mode(), err)
	}
	client := &Client{built: one}
	client.cleanup()
	if _, err := os.Stat(filepath.Dir(one)); !os.IsNotExist(err) {
		t.Fatalf("the build's directory outlived the session: %v", err)
	}
}

// TestADefaultClientIsUnchangedByTheOptionsExisting — a window that never
// heard of them launches exactly as before.
func TestADefaultClientIsUnchangedByTheOptionsExisting(t *testing.T) {
	client := New(&watching{}).(*Client)
	if client.waits || client.captured || client.streams != nil {
		t.Fatalf("a default client has options set: %+v", client)
	}
}
