package ide

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

// The IDE in a test: fakes behind every port, driven by key events and read
// back as a screen of text.

// fakeTerminal is a terminal nobody is sitting at.
type fakeTerminal struct {
	events chan term.Event
	w, h   int
	frames [][]byte
	lent   int
	failOn error
}

func (f *fakeTerminal) Events() <-chan term.Event { return f.events }
func (f *fakeTerminal) Size() (int, int)          { return f.w, f.h }
func (f *fakeTerminal) Show(frame []byte) error {
	f.frames = append(f.frames, frame)
	return f.failOn
}
func (f *fakeTerminal) Lend(use func()) error {
	f.lent++
	use()
	return nil
}

// fakeFiles is a disk in memory.
type fakeFiles struct {
	files   map[string]string
	failing map[string]error
	// readOnly is the files that can be read and not written.
	readOnly map[string]bool
	writes   []string
}

func (f *fakeFiles) Read(path string) (string, error) {
	if err := f.failing[path]; err != nil {
		return "", err
	}
	text, ok := f.files[path]
	if !ok {
		return "", fs.ErrNotExist
	}
	return text, nil
}

func (f *fakeFiles) Write(path, text string) error {
	if err := f.failing[path]; err != nil {
		return err
	}
	if f.readOnly[path] {
		return errors.New("read-only")
	}
	f.files[path] = text
	f.writes = append(f.writes, path)
	return nil
}

func (f *fakeFiles) List(dir string) ([]Entry, error) {
	if err := f.failing[dir]; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Entry
	for path := range f.files {
		rel, err := filepath.Rel(dir, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		name, _, nested := strings.Cut(rel, "/")
		if !seen[name] {
			seen[name] = true
			out = append(out, Entry{Name: name, Dir: nested})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (f *fakeFiles) Project(dir string) string { return dir }

func (f *fakeFiles) Sources(root string) ([]string, error) {
	if err := f.failing[root]; err != nil {
		return nil, err
	}
	var out []string
	for path := range f.files {
		if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out, nil
}

// fakeTidier cleans up the way a test says.
type fakeTidier struct {
	tidy  func(path, text string) (string, error)
	paths []string
}

func (f *fakeTidier) Tidy(path, text string) (string, error) {
	f.paths = append(f.paths, path)
	if f.tidy == nil {
		return text, nil
	}
	return f.tidy(path, text)
}

// fakeIntel knows what a test told it.
type fakeIntel struct {
	heard     func(string, []Problem)
	log       []string
	texts     map[string]string
	offers    []Suggestion
	asked     []edit.Pos
	place     Place
	found     bool
	about     string
	stopped   bool
	holdOffer func([]Suggestion)
	hold      bool
}

func (f *fakeIntel) Listen(heard func(string, []Problem)) { f.heard = heard }
func (f *fakeIntel) Open(path, text string) {
	f.log = append(f.log, "open "+filepath.Base(path))
	f.texts[path] = text
}
func (f *fakeIntel) Change(path, text string) {
	f.log = append(f.log, "change "+filepath.Base(path))
	f.texts[path] = text
}
func (f *fakeIntel) Save(path, _ string) { f.log = append(f.log, "save "+filepath.Base(path)) }
func (f *fakeIntel) Close(path string)   { f.log = append(f.log, "close "+filepath.Base(path)) }
func (f *fakeIntel) Complete(_ string, at edit.Pos, done func([]Suggestion)) {
	f.asked = append(f.asked, at)
	if f.hold {
		f.holdOffer = done
		return
	}
	done(f.offers)
}
func (f *fakeIntel) Define(_ string, _ edit.Pos, done func(Place, bool)) { done(f.place, f.found) }
func (f *fakeIntel) Describe(_ string, _ edit.Pos, done func(string))    { done(f.about) }
func (f *fakeIntel) Stop()                                               { f.stopped = true }

// fakeRunner runs nothing and lets a test say what the job printed.
type fakeRunner struct {
	jobs       []Job
	line       func(string)
	done       func(error)
	stopped    int
	foreground []Job
	failing    error
}

func (f *fakeRunner) Capture(_ context.Context, job Job, line func(string), done func(error)) func() {
	f.jobs = append(f.jobs, job)
	f.line, f.done = line, done
	return func() { f.stopped++ }
}

func (f *fakeRunner) Foreground(_ context.Context, job Job) error {
	f.foreground = append(f.foreground, job)
	return f.failing
}

// fakeDebugger debugs nothing and records what it was asked.
type fakeDebugger struct {
	mu       sync.Mutex
	watcher  debug.Watcher
	log      []string
	points   map[string][]debug.Breakpoint
	frames   []debug.Frame
	vars     map[int][]debug.Variable
	values   map[string]debug.Variable
	failing  error
	refuse   bool
	launched []string
}

func (f *fakeDebugger) note(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, s)
}

func (f *fakeDebugger) Launch(_ context.Context, program string, args []string, dir string) error {
	f.note("launch")
	f.launched = append([]string{program, dir}, args...)
	if f.failing != nil {
		return f.failing
	}
	f.watcher.Changed(debug.Session{State: debug.Starting})
	return nil
}

func (f *fakeDebugger) SetBreakpoints(_ context.Context, file string, points []debug.Breakpoint) ([]debug.Breakpoint, error) {
	f.note("breakpoints " + filepath.Base(file))
	out := make([]debug.Breakpoint, len(points))
	for i, p := range points {
		p.File, p.Verified = file, !f.refuse
		if f.refuse {
			p.Message = "no code here"
		}
		out[i] = p
	}
	f.points[file] = out
	return out, nil
}

func (f *fakeDebugger) resume(name string) error {
	f.note(name)
	f.watcher.Changed(debug.Session{State: debug.Running})
	return nil
}

func (f *fakeDebugger) Continue(context.Context) error { return f.resume("continue") }
func (f *fakeDebugger) Step(context.Context) error     { return f.resume("step") }
func (f *fakeDebugger) StepIn(context.Context) error   { return f.resume("stepin") }
func (f *fakeDebugger) StepOut(context.Context) error  { return f.resume("stepout") }
func (f *fakeDebugger) Frames(context.Context) ([]debug.Frame, error) {
	return f.frames, nil
}
func (f *fakeDebugger) Variables(_ context.Context, ref int) ([]debug.Variable, error) {
	return f.vars[ref], nil
}
func (f *fakeDebugger) Evaluate(_ context.Context, expression string, _ int) (debug.Variable, error) {
	if v, ok := f.values[expression]; ok {
		return v, nil
	}
	return debug.Variable{}, debug.ErrNotDebugging
}
func (f *fakeDebugger) Stop(context.Context) error {
	f.note("stop")
	f.watcher.Changed(debug.Session{State: debug.Ended})
	return nil
}

// stops makes the fake program stop at a line of a file.
func (f *fakeDebugger) stops(file string, line int) {
	f.frames = []debug.Frame{{ID: 1, Name: "main.main", File: file, Line: line, Scope: 100}, {ID: 2, Name: "runtime.main"}}
	f.watcher.Changed(debug.Session{State: debug.Stopped})
}

// rig is the IDE with its fakes.
type rig struct {
	t        *testing.T
	app      *App
	terminal *fakeTerminal
	files    *fakeFiles
	tidier   *fakeTidier
	intel    *fakeIntel
	runner   *fakeRunner
	debugger *fakeDebugger
	// rerooted is the projects the IDE moved to; detached what it said of
	// each project it entered.
	rerooted []string
	detached []bool
}

const root = "/work/demo"

const mainSource = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n"

// start builds the IDE over a small project.
func start(t *testing.T) *rig {
	t.Helper()
	return startIn(t, root, "", map[string]string{
		root + "/go.mod":           "module demo\n",
		root + "/main.go":          mainSource,
		root + "/shapes/shapes.go": "package shapes\n\nfunc Square(n int) int {\n\treturn n * n\n}\n",
		root + "/notes.txt":        "plain text\n",
	})
}

// startIn builds the IDE in a directory of a disk holding some files, on a
// machine whose user set GOWORK to something, or to nothing.
func startIn(t *testing.T, at, gowork string, files map[string]string) *rig {
	t.Helper()
	r := &rig{
		t:        t,
		terminal: &fakeTerminal{events: make(chan term.Event, 16), w: 100, h: 30},
		files:    &fakeFiles{failing: map[string]error{}, readOnly: map[string]bool{}, files: files},
		tidier:   &fakeTidier{},
		intel:    &fakeIntel{texts: map[string]string{}},
		runner:   &fakeRunner{},
		debugger: &fakeDebugger{points: map[string][]debug.Breakpoint{}, vars: map[int][]debug.Variable{}, values: map[string]debug.Variable{}},
	}
	r.app = New(context.Background(), Deps{
		Terminal: r.terminal, Files: r.files, Tidier: r.tidier, Intel: r.intel, Runner: r.runner,
		NewDebugger: func(w debug.Watcher) debug.Debugger {
			r.debugger.watcher = w
			return r.debugger
		},
		Root: at, Spawn: func(work func()) { work() }, Version: "version test",
		Gowork: gowork, Detach: func(detached bool) { r.detached = append(r.detached, detached) },
		Reroot: func(to string) Intel {
			r.rerooted = append(r.rerooted, to)
			r.intel = &fakeIntel{texts: map[string]string{}}
			return r.intel
		},
	})
	return r
}

// open opens a file of the project.
func (r *rig) open(name string) *rig {
	r.t.Helper()
	if err := r.app.Open(root + "/" + name); err != nil {
		r.t.Fatalf("opening %s: %v", name, err)
	}
	return r
}

// send gives the IDE an event and lets posted work run.
func (r *rig) send(ev term.Event) *rig {
	r.app.Handle(ev)
	r.app.Drain()
	return r
}

// press presses keys that are not characters.
func (r *rig) press(keys ...term.KeyCode) *rig {
	for _, k := range keys {
		r.send(term.Event{Kind: term.EvKey, Key: k})
	}
	return r
}

// chord presses a key with modifiers held.
func (r *rig) chord(k term.KeyCode, m term.Mods) *rig {
	return r.send(term.Event{Kind: term.EvKey, Key: k, Mods: m})
}

// ctrl presses Ctrl with a letter; alt presses Alt with one.
func (r *rig) ctrl(letter rune) *rig {
	return r.send(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: letter, Mods: term.Ctrl})
}

func (r *rig) alt(letter rune) *rig {
	return r.send(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: letter, Mods: term.Alt})
}

// write types characters.
func (r *rig) write(text string) *rig {
	for _, ch := range text {
		r.send(term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: ch})
	}
	return r
}

// screen draws the IDE and answers the picture as text.
func (r *rig) screen() string {
	r.t.Helper()
	if err := r.app.Paint(); err != nil {
		r.t.Fatalf("Paint: %v", err)
	}
	return r.app.Text()
}

// sees fails unless the screen shows every one of some texts.
func (r *rig) sees(wants ...string) *rig {
	r.t.Helper()
	got := r.screen()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			r.t.Fatalf("the screen does not show %q:\n%s", want, got)
		}
	}
	return r
}

// lacks fails when the screen shows any of some texts.
func (r *rig) lacks(unwanted ...string) *rig {
	r.t.Helper()
	got := r.screen()
	for _, u := range unwanted {
		if strings.Contains(got, u) {
			r.t.Fatalf("the screen shows %q:\n%s", u, got)
		}
	}
	return r
}

// page is a page of the bottom window as text, whether or not it fits the
// window.
func (r *rig) page(kind paneKind) string {
	var sb strings.Builder
	for _, row := range r.app.pane.rows[kind] {
		sb.WriteString(row.text + "\n")
	}
	return sb.String()
}

// text is the text being edited.
func (r *rig) text() string {
	r.t.Helper()
	ed := r.app.editor()
	if ed == nil {
		r.t.Fatal("no text is being edited")
	}
	return ed.Buf.Text()
}

// caret puts the caret at a line and column of the text being edited.
func (r *rig) caret(line, col int) *rig {
	r.app.editor().Goto(edit.Pos{Line: line, Col: col})
	return r
}

// errTerminalGone is a terminal that cannot be written to.
var errTerminalGone = errors.New("terminal gone")

// treeWidth is how wide the project tree is on the rig's 100-column
// terminal: the text's window starts after it.
const treeWidth = 25
