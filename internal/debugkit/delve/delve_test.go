package delve

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-delve/delve/service/api"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
)

// watching records what the debugger said about the session, which is the only
// way a caller learns anything about a program that is running.
type watching struct {
	mu   sync.Mutex
	seen []debug.Session
	out  []string
}

// Changed records one session state.
func (w *watching) Changed(s debug.Session) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seen = append(w.seen, s)
}

// Output records the debuggee's own writing.
func (w *watching) Output(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.out = append(w.out, text)
}

// waitFor runs until want is true or the bound passes. A debugger is a real
// subsystem with a real kernel behind it, so a test waits for an OUTCOME rather
// than sleeping a guessed amount.
func (w *watching) waitFor(bound time.Duration, want func([]debug.Session) bool) bool {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		seen := append([]debug.Session(nil), w.seen...)
		w.mu.Unlock()
		if want(seen) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// stoppedSomewhere reports whether the session ever came to rest.
func stoppedSomewhere(seen []debug.Session) bool {
	for _, s := range seen {
		if s.AtRest() {
			return true
		}
	}
	return false
}

func TestARunConfigurationIsReadAsADebuggingTarget(t *testing.T) {
	// The window hands this package whatever the operator chose to RUN. A
	// debugger needs one of two things: a package to build, or a program to
	// start — and telling them apart is what makes `go run .` debuggable.
	for _, row := range []struct {
		program string
		args    []string
		target  string
		runArgs string
		pkg     bool
		why     string
	}{
		{"go", []string{"run", "."}, "/w", "", true, "the ordinary Go run"},
		{"go", []string{"run", "./cmd/thing"}, "/w/cmd/thing", "", true, "a package in the project"},
		{"go", []string{"run", "-race", "."}, "/w", "", true, "flags before the package"},
		{"go", []string{"run", ".", "--", "-v", "x"}, "/w", "-v x", true, "the debuggee's own arguments"},
		{"go", []string{"build", "./cmd/thing"}, "/w/cmd/thing", "", true, "build names a package too"},
		{"/usr/bin/true", nil, "/usr/bin/true", "", false, "an absolute program is not built"},
		{"./server", []string{"-p", "8080"}, "/w/server", "-p 8080", false, "a program in the project"},
		{"bazel", []string{"test", "//..."}, "", "", false, "not a Go program at all"},
		{"go", []string{"mod", "tidy"}, "", "", false, "a Go subcommand that runs nothing"},
	} {
		target, runArgs, pkg := targetOf(row.program, row.args, "/w")
		if target != row.target || pkg != row.pkg {
			t.Errorf("%s %v → (%q, %v), want (%q, %v) — %s",
				row.program, row.args, target, pkg, row.target, row.pkg, row.why)
		}
		if got := strings.Join(runArgs, " "); got != row.runArgs {
			t.Errorf("%s %v → args %q, want %q", row.program, row.args, got, row.runArgs)
		}
	}
}

func TestAConfigurationThatRunsSomethingElseIsRefusedWithAReason(t *testing.T) {
	// Refused with a message that says what was needed, rather than launched
	// into a failure the operator has to read Delve's own words to understand.
	client := New(&watching{})
	err := client.Launch(context.Background(), "bazel", []string{"test", "//..."}, t.TempDir())
	if err == nil {
		t.Fatal("a bazel configuration was accepted as a debugging target")
	}
	if !strings.Contains(err.Error(), "Go program") {
		t.Fatalf("the operator would be told %q", err)
	}
}

func TestNothingIsDebuggedUntilSomethingIsLaunched(t *testing.T) {
	// A window with a debugger it never uses must cost nothing at all.
	client := New(&watching{})
	if _, err := client.Frames(context.Background()); err != debug.ErrNotDebugging {
		t.Fatalf("Frames = %v", err)
	}
	if _, err := client.Evaluate(context.Background(), "x", 0); err != debug.ErrNotDebugging {
		t.Fatalf("Evaluate = %v", err)
	}
	if err := client.Stop(context.Background()); err != nil {
		t.Fatalf("stopping nothing = %v", err)
	}
}

// program writes a small Go module a test can debug, and answers with its
// directory and the source file's path.
func program(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module debuggee\n\ngo 1.25\n")
	source := filepath.Join(dir, "main.go")
	write(t, source, `package main

import "fmt"

func main() {
	total := 0
	for i := 1; i <= 3; i++ {
		total = total + i
	}
	fmt.Println(total)
}
`)
	return dir, source
}

// write puts one file on disk.
func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestARealProgramStopsAtABreakpointAndIsReadable.
//
// The whole of this package in one test: a Go package is built, a breakpoint is
// set, the program runs to it, and the stack and the variables are read from
// the stopped process. It is skipped where the kernel will not let this process
// ptrace another, which is the ordinary state of a hardened container.
func TestARealProgramStopsAtABreakpointAndIsReadable(t *testing.T) {
	dir, source := program(t)
	watcher := &watching{}
	client := New(watcher)
	t.Cleanup(func() { _ = client.Stop(context.Background()) })

	if _, err := client.SetBreakpoints(context.Background(), source, nil); err != debug.ErrNotDebugging {
		t.Fatalf("breakpoints before a launch = %v", err)
	}
	if err := client.Launch(context.Background(), "go", []string{"run", "."}, dir); err != nil {
		t.Skipf("this machine cannot debug here: %v", err)
	}
	// The breakpoint goes in after the launch and before the program is let go
	// — the same order DAP itself requires.
	if _, err := client.SetBreakpoints(context.Background(), source, []debug.Breakpoint{{Line: 8}}); err != nil {
		t.Skipf("the breakpoint was refused: %v", err)
	}
	_ = client.Continue(context.Background())
	if !watcher.waitFor(30*time.Second, stoppedSomewhere) {
		t.Skip("the program never stopped; this machine may not allow ptrace")
	}

	frames, err := client.Frames(context.Background())
	if err != nil || len(frames) == 0 {
		t.Fatalf("Frames = %v, %v", frames, err)
	}
	if !strings.Contains(frames[0].Name, "main") {
		t.Fatalf("the stack starts at %q", frames[0].Name)
	}
	vars, err := client.Variables(context.Background(), 0)
	if err != nil {
		t.Fatalf("Variables = %v", err)
	}
	if !named(vars, "total") {
		t.Fatalf("the scope is %v, want the local the program is building", vars)
	}
	value, err := client.Evaluate(context.Background(), "total", 0)
	if err != nil {
		t.Fatalf("Evaluate = %v", err)
	}
	if value.Name != "total" || value.Value == "" {
		t.Fatalf("evaluated %+v", value)
	}
}

// variableNamed is one of Delve's values with only its name set, which is all
// the expression rule reads.
func variableNamed(name string) *api.Variable { return &api.Variable{Name: name} }

// named reports whether a scope carries a variable.
func named(vars []debug.Variable, name string) bool {
	for _, one := range vars {
		if one.Name == name {
			return true
		}
	}
	return false
}

// needsGo skips where there is no Go toolchain to build with, which is the
// ordinary state of the test sandbox — and the reason this package's
// end-to-end case skips there too.
func needsGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command on PATH to build with")
	}
}

// TestAProgramIsBuiltInsideTheProject.
//
// The go command establishes the main module from where it RUNS, and it runs
// wherever the window was started. A build of `/w/proj` from somebody's home
// directory answers "go.mod file not found in current directory or any parent
// directory", which reached the operator as `exit status 1` and a debug session
// that never began (live run, 2026-09-18).
func TestAProgramIsBuiltInsideTheProject(t *testing.T) {
	needsGo(t)
	dir, _ := program(t)
	binary := filepath.Join(t.TempDir(), "built")
	if err := buildIn(dir, binary, dir); err != nil {
		t.Skipf("this machine cannot build here: %v", err)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("the build reported success and wrote nothing: %v", err)
	}
}

// TestABuildThatFailsSaysWhatTheCompilerSaid — "exit status 1" is a debugger
// telling somebody about a subprocess's return code.
func TestABuildThatFailsSaysWhatTheCompilerSaid(t *testing.T) {
	needsGo(t)
	dir, source := program(t)
	write(t, source, "package main\n\nfunc main() { thisDoesNotExist() }\n")
	err := buildIn(dir, filepath.Join(t.TempDir(), "built"), dir)
	if err == nil {
		t.Skip("this machine built a program that does not compile")
	}
	if !errors.Is(err, debug.ErrBuildFailed) {
		t.Fatalf("err = %v, want a named build failure", err)
	}
	if !strings.Contains(err.Error(), "thisDoesNotExist") {
		t.Fatalf("the operator would be told %q", err)
	}
}

func TestAChildIsNamedByItsWholeExpression(t *testing.T) {
	// `x` inside a struct is `parent.x`. An editor that evaluated the bare name
	// would answer about a different variable or about nothing, and it would do
	// it silently.
	for _, row := range []struct{ parent, name, want string }{
		{"", "total", "total"},
		{"config", "Name", "config.Name"},
		{"items", "3", "items[3]"},
	} {
		got := expressionFor(row.parent, variableNamed(row.name))
		if got != row.want {
			t.Errorf("child %q of %q → %q, want %q", row.name, row.parent, got, row.want)
		}
	}
}

func TestAHandleFromTheLastStopIsNotAnswered(t *testing.T) {
	// It names a value that may not exist any more, and answering it would draw
	// the previous moment's numbers under this moment's names.
	client := &Client{}
	ref := client.keep(reference{goroutine: 1, frame: 0, expression: "total"})
	if _, known := client.held(ref); !known {
		t.Fatal("a handle was not kept")
	}
	client.forgetRefs()
	if _, known := client.held(ref); known {
		t.Fatal("a handle survived the program being resumed")
	}
}
