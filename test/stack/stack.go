// Package stack runs the real IDE for the black-box suites: the same
// wiring the binary uses (compose.Build), over a real project on disk, the
// real Go tool, the real language server and the real debugger — with only
// the terminal replaced, by one a test types into and reads.
package stack

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/compose"
	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
)

// Terminal is a terminal nobody sits at: it has a size, keeps what was
// written to it, and lends itself at once.
type Terminal struct {
	W, H int

	mu     sync.Mutex
	frames [][]byte
	lent   int
}

// Events is never read: a test hands events to the IDE itself.
func (t *Terminal) Events() <-chan term.Event { return nil }

// Size is the terminal's size.
func (t *Terminal) Size() (int, int) { return t.W, t.H }

// Show keeps a frame.
func (t *Terminal) Show(frame []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.frames = append(t.frames, append([]byte(nil), frame...))
	return nil
}

// Lend runs what the terminal is lent to.
func (t *Terminal) Lend(use func()) error {
	t.mu.Lock()
	t.lent++
	t.mu.Unlock()
	use()
	return nil
}

// Written is every byte the IDE sent to the terminal.
func (t *Terminal) Written() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return bytes.Join(t.frames, nil)
}

// Stack is the IDE running over a project.
type Stack struct {
	T        *testing.T
	App      *ide.App
	Dir      string
	Terminal *Terminal
	// Out is what a program run in the foreground wrote.
	Out *bytes.Buffer
}

// Options is how a stack differs from the usual one.
type Options struct {
	// Gopls is the language server to start; empty starts none.
	Gopls string
	// Go is the Go tool; empty means the machine's own.
	Go string
	// Bazel is the Bazel to use; empty means the machine's own.
	Bazel string
	// Sub is the directory, inside the files written, the IDE is started
	// in; empty starts it at their top.
	Sub string
}

// NeedsGo skips a test on a machine with no Go tool.
func NeedsGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no Go tool on this machine")
	}
}

// NeedsGopls finds the language server, or skips a test on a machine with
// none. A test run by Bazel is given a short PATH, so the places a Go
// developer's tools are usually installed are looked in too.
func NeedsGopls(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("gopls"); err == nil {
		return path
	}
	home := ""
	if me, err := user.Current(); err == nil {
		home = me.HomeDir
	}
	for _, dir := range []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, "go", "bin"), "/usr/local/bin"} {
		path := filepath.Join(dir, "gopls")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	t.Skip("no gopls on this machine")
	return ""
}

// Start writes a project to a temporary directory and runs the IDE over
// it. The IDE is stopped when the test ends.
func Start(t *testing.T, files map[string]string, o Options) *Stack {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		Put(t, filepath.Join(dir, name), text)
	}
	// The IDE may set GOWORK for the tools it starts; the test process is
	// given back what it had when the test ends.
	t.Setenv("GOWORK", os.Getenv("GOWORK"))
	dir = filepath.Join(dir, o.Sub)
	ctx, cancel := context.WithCancel(context.Background())
	s := &Stack{T: t, Dir: dir, Terminal: &Terminal{W: 110, H: 34}, Out: &bytes.Buffer{}}
	o = withDefaults(o)
	s.App = compose.Build(ctx, compose.Options{
		Terminal: s.Terminal, Dir: dir, Go: o.Go, Gopls: o.Gopls, Bazel: o.Bazel, Debugger: true,
		In: strings.NewReader(strings.Repeat("\n", 64)), Out: s.Out, Err: s.Out, Version: "version test",
	})
	t.Cleanup(func() {
		s.App.Do("stop")
		s.App.Shutdown()
		cancel()
	})
	return s
}

// withDefaults is options with the machine's own tools where none was
// named.
func withDefaults(o Options) Options {
	if o.Bazel == "" {
		o.Bazel = "bazel"
	}
	if o.Go == "" {
		o.Go = "go"
	}
	return o
}

// Put writes a file, making the directories above it.
func Put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Path is a file of the project.
func (s *Stack) Path(name string) string { return filepath.Join(s.Dir, name) }

// File is what a file of the project holds on disk.
func (s *Stack) File(name string) string {
	s.T.Helper()
	data, err := os.ReadFile(s.Path(name))
	if err != nil {
		s.T.Fatal(err)
	}
	return string(data)
}

// Open opens a file of the project.
func (s *Stack) Open(name string) *Stack {
	s.T.Helper()
	if err := s.App.Open(s.Path(name)); err != nil {
		s.T.Fatalf("opening %s: %v", name, err)
	}
	return s
}

// Send gives the IDE an event and lets the work it caused run.
func (s *Stack) Send(ev term.Event) *Stack {
	s.App.Handle(ev)
	s.App.Drain()
	return s
}

// Press presses keys that are not characters.
func (s *Stack) Press(keys ...term.KeyCode) *Stack {
	for _, k := range keys {
		s.Send(term.Event{Kind: term.EvKey, Key: k})
	}
	return s
}

// Chord presses a key with modifiers held.
func (s *Stack) Chord(k term.KeyCode, m term.Mods) *Stack {
	return s.Send(term.Event{Kind: term.EvKey, Key: k, Mods: m})
}

// Ctrl presses Ctrl with a letter.
func (s *Stack) Ctrl(letter rune) *Stack {
	return s.Send(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: letter, Mods: term.Ctrl})
}

// Alt presses Alt with a letter.
func (s *Stack) Alt(letter rune) *Stack {
	return s.Send(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: letter, Mods: term.Alt})
}

// Type types characters.
func (s *Stack) Type(text string) *Stack {
	for _, r := range text {
		s.Send(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: r})
	}
	return s
}

// Raw feeds the IDE bytes as a terminal would send them, through the same
// parser the real terminal's reader uses.
func (s *Stack) Raw(input string) *Stack {
	events, _ := term.Parse([]byte(input), true)
	for _, ev := range events {
		s.Send(ev)
	}
	return s
}

// GoTo puts the caret on a line and column, both counted from one, through
// the IDE's own dialog.
func (s *Stack) GoTo(line string) *Stack {
	s.Ctrl('g')
	s.Ctrl('a')
	for i := 0; i < 12; i++ {
		s.Press(term.KeyBackspace)
	}
	return s.Type(line).Press(term.KeyEnter)
}

// Screen draws the IDE and answers the picture as text.
func (s *Stack) Screen() string {
	s.T.Helper()
	s.App.Drain()
	if err := s.App.Paint(); err != nil {
		s.T.Fatalf("Paint: %v", err)
	}
	return s.App.Text()
}

// Sees fails unless the screen shows every one of some texts now.
func (s *Stack) Sees(wants ...string) *Stack {
	s.T.Helper()
	got := s.Screen()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			s.T.Fatalf("the screen does not show %q:\n%s", want, got)
		}
	}
	return s
}

// Lacks fails when the screen shows any of some texts.
func (s *Stack) Lacks(unwanted ...string) *Stack {
	s.T.Helper()
	got := s.Screen()
	for _, u := range unwanted {
		if strings.Contains(got, u) {
			s.T.Fatalf("the screen shows %q:\n%s", u, got)
		}
	}
	return s
}

// Shows waits for the screen to show a text and reports whether it did.
// The compiler, the language server and the debugger answer when they
// answer; a test waits for an outcome rather than sleeping a guess.
func (s *Stack) Shows(want string, within time.Duration) bool {
	s.T.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if strings.Contains(s.Screen(), want) {
			return true
		}
		time.Sleep(15 * time.Millisecond)
	}
	return false
}

// Awaits fails unless the screen comes to show a text.
func (s *Stack) Awaits(want string, within time.Duration) *Stack {
	s.T.Helper()
	if !s.Shows(want, within) {
		s.T.Fatalf("the screen never showed %q:\n%s", want, s.Screen())
	}
	return s
}

// Binary is the real program, for the tests that run it. It is named by
// LAZURITE_BIN: an absolute path (`go build -o lazurite ./cmd/lazurite`
// then `LAZURITE_BIN=$PWD/lazurite go test ./test/...`), or a path among
// the files Bazel gives a test. A test is skipped when there is none.
func Binary(t *testing.T) string {
	t.Helper()
	path := os.Getenv("LAZURITE_BIN")
	if path == "" {
		t.Skip("LAZURITE_BIN does not name the built binary")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(os.Getenv("TEST_SRCDIR"), os.Getenv("TEST_WORKSPACE"), path)
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		t.Skipf("the binary is not at %s", path)
	}
	return path
}
