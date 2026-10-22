package ide

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mateuszmierzwinski/lazurite/internal/monokit/bazeltarget"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/ui"
)

// The project: its files as a tree at the left of the desktop, moving the
// IDE to another project, and making a new one.

// rebuildTree lists the project again: the root's entries, and under each
// open directory its own.
func (a *App) rebuildTree() {
	a.tree.nodes = a.tree.nodes[:0]
	a.grow(a.root, 0)
	rows := make([]string, len(a.tree.nodes))
	for i, n := range a.tree.nodes {
		rows[i] = a.nodeText(n)
	}
	a.tree.list.Set(rows)
}

// grow adds a directory's entries to the tree, and those of the open
// directories among them. A directory that cannot be read adds nothing.
func (a *App) grow(dir string, depth int) {
	entries, err := a.deps.Files.List(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if len(a.tree.nodes) >= maxTreeRows {
			return
		}
		path := filepath.Join(dir, e.Name)
		a.tree.nodes = append(a.tree.nodes, node{path: path, dir: e.Dir, depth: depth})
		if e.Dir && a.tree.open[path] {
			a.grow(path, depth+1)
		}
	}
}

// nodeText is one row of the tree: indented by its depth, a directory
// marked open or closed.
func (a *App) nodeText(n node) string {
	mark := "  "
	switch {
	case n.dir && a.tree.open[n.path]:
		mark = "▾ "
	case n.dir:
		mark = "▸ "
	}
	name := filepath.Base(n.path)
	if n.dir {
		name += "/"
	}
	return strings.Repeat("  ", n.depth) + mark + name
}

// chosenNode is the row of the tree that is chosen.
func (a *App) chosenNode() (node, bool) {
	at := a.tree.list.Sel
	if at < 0 || at >= len(a.tree.nodes) {
		return node{}, false
	}
	return a.tree.nodes[at], true
}

// toggleTree shows the tree and gives it the keyboard; asked again while
// it has it, hides it.
func (a *App) toggleTree() {
	if a.tree.shown && a.focus == focusTree {
		a.tree.shown, a.focus = false, focusText
		return
	}
	a.tree.shown, a.focus = true, focusTree
	a.rebuildTree()
}

// treeKey gives a key to the tree and reports whether it was the tree's.
func (a *App) treeKey(ev term.Event) bool {
	areas := a.layout()
	switch {
	case a.tree.list.Key(ev, max(areas.tree.H-3, 1)):
	case ev.Key == term.KeyEnter:
		a.treeEnter()
	case ev.Key == term.KeyRight && ev.Mods == 0:
		a.setOpen(true)
	case ev.Key == term.KeyLeft && ev.Mods == 0:
		a.setOpen(false)
	default:
		return false
	}
	return true
}

// treeEnter opens the chosen file, or opens and closes the chosen
// directory.
func (a *App) treeEnter() {
	n, ok := a.chosenNode()
	if !ok {
		return
	}
	if n.dir {
		a.tree.open[n.path] = !a.tree.open[n.path]
		a.rebuildTree()
		return
	}
	a.open(n.path)
}

// setOpen opens or closes the chosen directory. Closing something that is
// not an open directory goes to the directory it is in instead.
func (a *App) setOpen(open bool) {
	n, ok := a.chosenNode()
	if !ok {
		return
	}
	if n.dir && a.tree.open[n.path] != open {
		a.tree.open[n.path] = open
		a.rebuildTree()
		return
	}
	if !open {
		a.chooseParent(n)
	}
}

// chooseParent moves the choice to the directory a row is in.
func (a *App) chooseParent(n node) {
	parent := filepath.Dir(n.path)
	for i, other := range a.tree.nodes {
		if other.path == parent {
			a.tree.list.Sel = i
			return
		}
	}
}

// treeMouse is the mouse over the tree: a press chooses a row and acts on
// it as Enter does; the wheel scrolls.
func (a *App) treeMouse(ev term.Event, window term.Rect) {
	switch ev.Button {
	case term.MouseLeft:
		a.focus = focusTree
		if a.tree.list.Click(ev.Y - window.Y - 1) {
			a.treeEnter()
		}
	case term.WheelUp:
		a.tree.list.Move(-wheelLines)
	case term.WheelDown:
		a.tree.list.Move(wheelLines)
	}
}

// openProjectDialog asks which directory to move to.
func (a *App) openProjectDialog() {
	a.prompt("Open project", "Directory of the project", a.root, func(typed string) {
		a.moveTo(a.absolute(typed), "")
	})
}

// moveTo moves the IDE to another project: every text is closed (each with
// unsaved edits asks first), and the tree, the language server and the
// debugger start afresh there. then, when given, is a file to open in it.
func (a *App) moveTo(dir, then string) {
	if _, err := a.deps.Files.List(dir); err != nil {
		a.fail(err)
		return
	}
	a.settleAllThen(func() {
		a.closeEverything()
		a.root = a.deps.Files.Project(dir)
		a.survey()
		a.reroot()
		a.tree.open, a.tree.list = map[string]bool{}, ui.List{}
		a.rebuildTree()
		a.say("Project: " + a.root + " — " + a.standing())
		if then != "" {
			a.open(then)
		}
	})
}

// closeEverything forgets what belonged to the project being left: its
// texts, its problems, its breakpoints, its running job and program.
func (a *App) closeEverything() {
	a.stopEverything()
	for _, d := range a.docs {
		a.intel.Close(d.path)
	}
	a.docs, a.at, a.untitled, a.target = nil, 0, 0, ""
	a.problems = map[string][]Problem{}
	a.dbg.breaks, a.dbg.stale = map[string]map[int]bool{}, map[string]bool{}
	a.rebuildMessages()
	a.clearOutput()
}

// reroot points the language intelligence at the project the IDE moved
// to. The one it had is stopped off the loop: stopping waits for the
// server to say goodbye.
func (a *App) reroot() {
	if a.deps.Reroot == nil {
		return
	}
	old := a.intel
	a.intel = a.deps.Reroot(a.root)
	a.intel.Listen(a.heard)
	a.warming = a.deps.Warming
	a.spawn(old.Stop)
}

// newProjectDialog asks where a new project should be made and what its
// module is called, and makes it.
func (a *App) newProjectDialog() {
	start := filepath.Join(filepath.Dir(a.root), "newproject")
	a.prompt("New Go project", "Directory to create", start, func(typed string) {
		dir := a.absolute(typed)
		a.prompt("New Go project", "Module path (as in go.mod)", filepath.Base(dir), func(module string) {
			a.newProject(dir, module)
		})
	})
}

// newProject makes a project — a directory with a main.go, a README and a
// .gitignore — has the Go tool write its go.mod, and moves the IDE into
// it. A directory that already holds something is refused.
//
// Made inside a monorepo it is made the monorepo's way: under a Bazel
// workspace it gets a BUILD file declaring its binary, and under a go.work
// that does not list it the user is asked whether to add it.
func (a *App) newProject(dir, module string) {
	if entries, err := a.deps.Files.List(dir); err == nil && len(entries) > 0 {
		a.fail(ErrNotEmpty)
		return
	}
	if a.stop != nil {
		a.fail(ErrBusy)
		return
	}
	name := filepath.Base(dir)
	files := projectFiles(name)
	if a.above(dir, bazeltarget.Markers()) != "" {
		files["BUILD"] = buildFile(name, module)
	}
	for file, text := range files {
		if err := a.deps.Files.Write(filepath.Join(dir, file), text); err != nil {
			a.fail(err)
			return
		}
	}
	job := Job{Dir: dir, Args: []string{"mod", "init", module}}
	a.stop = a.deps.Runner.Capture(a.ctx, job,
		func(line string) { a.post(func() { a.printFrom(dir, line) }) },
		func(err error) { a.post(func() { a.projectMade(dir, module, err) }) })
}

// projectMade finishes a new project when the Go tool has written its
// go.mod — or writes a plain one itself when the tool could not — and
// moves into it with main.go open.
func (a *App) projectMade(dir, module string, err error) {
	a.stop = nil
	if err != nil {
		if werr := a.deps.Files.Write(filepath.Join(dir, "go.mod"), "module "+module+"\n\ngo "+fallbackGo+"\n"); werr != nil {
			a.fail(werr)
			return
		}
	}
	a.moveTo(dir, filepath.Join(dir, "main.go"))
	if a.root == dir {
		a.offerWork()
	}
}

// fallbackGo is the language version written to a go.mod the Go tool
// could not write itself.
const fallbackGo = "1.22"

// projectFiles is what a new project starts with, besides its go.mod. The
// name goes into main.go as a quoted string: a directory may be called
// anything, and the program must compile whatever it is called.
func projectFiles(name string) map[string]string {
	return map[string]string{
		"main.go":    "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(" + strconv.Quote("Hello from "+name) + ")\n}\n",
		"README.md":  "# " + name + "\n\nA Go program.\n\n## Run\n\n    go run .\n\n## Test\n\n    go test ./...\n",
		".gitignore": "/" + name + "\n*.test\n*.out\n",
	}
}
