// Package ide is Lazurite itself: the texts that are open, what each key
// and menu does, building, running, testing and debugging, and how it is
// all drawn.
//
// It is the core of the hexagon. It knows no terminal, disk, language
// server, compiler or debugger — only the ports in interfaces.go — so the
// whole IDE runs in a test with fakes behind them, driven by key events and
// read back as a screen.
package ide

import (
	"context"
	"sync"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/lexer"
	"github.com/mateuszmierzwinski/lazurite/internal/monokit/bazeltarget"
	"github.com/mateuszmierzwinski/lazurite/internal/monokit/buildkind"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/ui"
)

// Brand is the product's name, here and nowhere else.
const Brand = "Lazurite"

// Entry is one name in a directory.
type Entry struct {
	Name string
	Dir  bool
}

// Problem is something wrong with a stretch of a file.
type Problem struct {
	From, To edit.Pos
	// Severe is an error; otherwise a warning.
	Severe  bool
	Message string
}

// Edit is a change to a text: what is between two positions gives way to
// another text.
type Edit struct {
	From, To edit.Pos
	Text     string
}

// Suggestion is one way to complete what is being typed.
type Suggestion struct {
	Label, Detail string
	// Insert is what replaces the word being typed.
	Insert string
	// Extra is what else choosing it changes: the import it needs.
	Extra []Edit
}

// Place is a position in a file.
type Place struct {
	Path string
	At   edit.Pos
}

// Job is one run of a build tool: its arguments, and where.
type Job struct {
	// Tool is which tool: empty for the Go tool, ToolBazel for Bazel.
	Tool string
	Dir  string
	Args []string
}

// ToolBazel names Bazel as the tool of a job.
const ToolBazel = "bazel"

// Deps is everything the IDE is built from.
type Deps struct {
	Terminal Terminal
	Files    Files
	Tidier   Tidier
	Intel    Intel
	Runner   Runner
	// NewDebugger makes the debugger, given who it reports to.
	NewDebugger func(debug.Watcher) debug.Debugger
	// Root is the project: the directory builds run in and paths are shown
	// relative to.
	Root string
	// Reroot makes the language intelligence of another project, for when
	// the IDE moves to one; nil keeps the one it has.
	Reroot func(root string) Intel
	// Notice is said on the status line at the start: what this machine
	// lacks.
	Notice string
	// Detach is told, each time the IDE enters a project, whether that
	// project is a Go module the go.work above it does not list. Such a
	// module is refused by the whole Go toolchain until it is built
	// standalone (GOWORK=off); whoever starts the tools arranges that.
	Detach func(detached bool)
	// Gowork is the user's own GOWORK setting when the IDE started. One
	// they set themselves is an answer already, and nothing overrides it.
	Gowork string
	// Warming says the language intelligence takes a while to load a
	// project before it answers: the status line then says it is loading,
	// and says when it is ready.
	Warming bool
	// Spawn runs work off the loop; nil starts a goroutine. A test passes
	// one that runs the work at once.
	Spawn func(work func())
	// Version is shown in the About box.
	Version string
}

// document is one text that is open.
type document struct {
	// path is where it lives; name is how it is shown. unnamed marks a
	// text never saved, whose path is only a place to be completed in.
	path, name string
	unnamed    bool
	ed         *edit.Editor
	// savedRev is the buffer's revision when it was last saved or loaded;
	// sentRev the one last told to the language server.
	savedRev, sentRev int
	// spans is the syntax colouring, made at spansRev.
	spans    [][]lexer.Span
	spansRev int
}

// changed reports whether a document has edits that are not saved.
func (d *document) changed() bool { return d.ed.Buf.Rev() != d.savedRev }

// paneKind is which of the bottom window's pages is shown.
type paneKind int

// The pages of the bottom window.
const (
	paneMessages paneKind = iota
	paneOutput
	paneDebug
	paneCount
)

// paneTitles is what each page is called.
var paneTitles = [paneCount]string{"Messages", "Output", "Debug"}

// row is one line of a page: text, and where Enter on it goes, if anywhere.
type row struct {
	text  string
	place Place
	jumps bool
	// variable is the index of the variable the row shows, plus one; zero
	// for a row that shows none.
	variable int
}

// pane is the bottom window: three pages, one shown.
type pane struct {
	kind  paneKind
	shown bool
	rows  [paneCount][]row
	lists [paneCount]ui.List
}

// popup is the list of completions being offered.
type popup struct {
	all, shown []Suggestion
	list       ui.List
	// from is where the word being completed starts; a caret that leaves
	// the word closes the list.
	from edit.Pos
}

// asked names a completion request: the word it was asked for.
type asked struct {
	path string
	from edit.Pos
}

// variable is one line of the debugger's variables: a value, how deep it is
// nested, and whether its children are shown.
type variable struct {
	v     debug.Variable
	depth int
	open  bool
}

// debugging is the state of the debugger as the IDE sees it.
type debugging struct {
	session debug.Session
	frames  []debug.Frame
	vars    []variable
	watches debug.Watches
	// resume is where the program rests; at is false when it does not.
	resume Place
	at     bool
	// breaks is the breakpoints, by file and line; stale the files whose
	// breakpoints the debugger has not been told of yet.
	breaks map[string]map[int]bool
	stale  map[string]bool
	// once is a breakpoint that exists only to stop a program at its first
	// line, and is removed when it has.
	once     Place
	onceSet  bool
	starting bool
}

// focus is which part of the desktop has the keyboard.
type focus int

// The parts that can have it.
const (
	focusText focus = iota
	focusPane
	focusTree
)

// project is what the IDE knows about where it is: what builds it, and
// how it sits in the workspaces around it.
type project struct {
	// kind is what the project is built with.
	kind buildkind.Kind
	// workspace is the root of the Bazel workspace the project is in, and
	// home the project's package path inside it; both empty outside one.
	workspace, home string
	// work is the directory of the go.work above the project, empty when
	// there is none; detached says that go.work does not list the project.
	work     string
	detached bool
}

// node is one row of the project tree: a file or a directory, and how deep.
type node struct {
	path  string
	dir   bool
	depth int
}

// tree is the project's files as a tree at the left of the desktop.
type tree struct {
	shown bool
	// open is the directories whose contents are shown.
	open  map[string]bool
	nodes []node
	list  ui.List
}

// App is the IDE.
type App struct {
	deps     Deps
	debugger debug.Debugger
	ctx      context.Context
	// root is the project: where builds run and what paths are shown
	// relative to. intel is what knows its language.
	root  string
	intel Intel
	focus focus
	tree  tree
	proj  project
	// targets is what the Bazel workspace can build, test and run, once
	// asked; runTarget is the label Run runs there.
	targets   []bazeltarget.Target
	listed    bool
	runTarget string

	screen, shown *term.Screen
	menu          ui.MenuBar
	docs          []*document
	at            int
	untitled      int

	dialog   ui.Dialog
	answered func(ui.Outcome)
	pane     pane
	popup    *popup
	asked    asked
	// wanted says the completions being waited for were asked for by key,
	// so that an empty answer is worth a word.
	wanted bool
	// warming says the language server has not answered anything yet.
	warming bool
	status  string
	clip    string
	find    string
	quit    bool
	// scrolled says the wheel moved the view off the caret; the next key
	// brings it back.
	scrolled bool

	// posted is work other goroutines hand to the loop: 256 deep so that a
	// burst of compiler output does not stall the goroutine reading it; a
	// full queue blocks the sender, which is the back-pressure wanted.
	posted chan func()
	done   chan struct{}
	// work does the debugger's calls off the loop, in order; it is started
	// by the first of them (offLoop) and ended by Shutdown (closing), which
	// closes stopped.
	work    *worker
	offLoop sync.Once
	closing sync.Once
	stopped chan struct{}

	problems map[string][]Problem
	// stop ends the job being captured; nil when none is.
	stop func()
	// target is the directory of the program Run runs; args what it is
	// given.
	target, args string
	dbg          debugging
}

// The limits of the IDE.
const (
	// maxOutputRows is how much output the bottom window remembers.
	maxOutputRows = 5000
	// maxSuggestions is how many completions are offered at once.
	maxSuggestions = 200
	// popupRows is how many of them show at once.
	popupRows = 8
	// maxDocuments is how many texts can be open.
	maxDocuments = 64
	// maxProblemRows is how many problems the Messages page lists.
	maxProblemRows = 1000
	// maxVariables is how many variable lines the Debug page shows.
	maxVariables = 500
	// maxTreeRows is how many rows the project tree shows.
	maxTreeRows = 5000
)
