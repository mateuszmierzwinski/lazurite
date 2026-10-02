package ide

import (
	"path/filepath"
	"strings"
)

// Building, testing and running: the Go tool, with its output caught in the
// bottom window or let onto the terminal itself.

// goJob builds or tests: through Bazel where Bazel builds the project and
// the job is one Bazel does, with the Go tool otherwise.
func (a *App) goJob(c goJob) {
	if a.bazel() && c.bazel != "" {
		a.capture(c.title, Job{Tool: ToolBazel, Dir: a.proj.workspace, Args: []string{c.bazel, a.pattern(c.whole)}})
		return
	}
	job := Job{Dir: a.root, Args: c.args}
	if !c.whole {
		job.Dir = a.packageDir()
	}
	a.capture(c.title, job)
}

// toolNames is what each tool is called in the output's first line.
var toolNames = map[string]string{"": "go", ToolBazel: "bazel"}

// capture runs a job and catches what it prints in the bottom window.
func (a *App) capture(title string, job Job) {
	if a.stop != nil {
		a.fail(ErrBusy)
		return
	}
	if !a.saveAll() {
		return
	}
	a.clearOutput()
	a.print(toolNames[job.Tool] + " " + strings.Join(job.Args, " ") + "   in " + displayName(a.root, job.Dir))
	a.showPane(paneOutput, false)
	a.say(title + "…")
	a.stop = a.deps.Runner.Capture(a.ctx, job,
		func(line string) { a.post(func() { a.printFrom(job.Dir, line) }) },
		func(err error) { a.post(func() { a.finished(title, err) }) })
}

// packageDir is the directory of the text being edited: the package a
// "package" job is about. With no file open it is the project.
func (a *App) packageDir() string {
	if d := a.doc(); d != nil {
		return filepath.Dir(d.path)
	}
	return a.root
}

// finished ends a job: says how it went, and after a failure goes to the
// first place its output named.
func (a *App) finished(title string, err error) {
	a.stop = nil
	if err == nil {
		a.print("✓ " + title + " succeeded")
		a.say(title + " succeeded")
		return
	}
	a.print("✗ " + title + " failed: " + oneLine(err.Error()))
	a.say(title + " failed — Alt+F8 goes to the next problem")
	rows := a.pane.rows[paneOutput]
	for i, r := range rows {
		if r.jumps {
			a.pane.lists[paneOutput].Sel = i
			a.jump(r.place)
			return
		}
	}
}

// stopEverything ends the job being captured and the debugged program.
func (a *App) stopEverything() {
	if a.stop != nil {
		a.stop()
		a.say("Stopping…")
	}
	a.stopDebugging()
}

// targetDir is the directory of the program Run and Debug start: the
// package of the text being edited when that is package main, else the one
// chosen before.
func (a *App) targetDir() (string, bool) {
	if d := a.doc(); d != nil && !d.unnamed && declaresMain(d.ed.Buf.Text()) {
		a.target = filepath.Dir(d.path)
	}
	if a.target == "" && a.rootIsProgram() {
		a.target = a.root
	}
	return a.target, a.target != ""
}

// rootIsProgram reports whether the project's own directory holds a
// program.
func (a *App) rootIsProgram() bool {
	_, ok := a.mainPlace(a.root)
	return ok
}

// mainPlace is where a directory's program begins: the line of its
// `func main`.
func (a *App) mainPlace(dir string) (Place, bool) {
	entries, err := a.deps.Files.List(dir)
	if err != nil {
		return Place{}, false
	}
	for _, e := range entries {
		if e.Dir || !strings.HasSuffix(e.Name, ".go") || strings.HasSuffix(e.Name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, e.Name)
		text, err := a.deps.Files.Read(path)
		if line, ok := mainLine(text); err == nil && ok {
			return Place{Path: path, At: posAtLine(line)}, true
		}
	}
	return Place{}, false
}

// runArgs is the arguments of `go run` for the program, with what the user
// wants the program itself given.
func (a *App) runArgs() []string {
	args := []string{"run", "."}
	if extra := strings.Fields(a.args); len(extra) > 0 {
		args = append(append(args, "--"), extra...)
	}
	return args
}

// runProgram runs the program on the terminal itself: the IDE steps aside,
// the program has the keyboard and the screen, and the IDE comes back when
// the user has seen it end. Where Bazel builds the project it is a target
// that is run — the one chosen before, or the one chosen now.
func (a *App) runProgram() {
	if a.bazel() {
		a.runBazel()
		return
	}
	dir, ok := a.targetDir()
	if !ok {
		a.fail(ErrNoTarget)
		return
	}
	if !a.saveAll() {
		return
	}
	a.lend(Job{Dir: dir, Args: a.runArgs()})
}

// runBazel runs the chosen target, or asks which when none was chosen.
func (a *App) runBazel() {
	if a.runTarget == "" {
		a.targetsDialog()
		return
	}
	if a.saveAll() {
		a.lend(a.bazelRun(a.runTarget))
	}
}

// userScreen shows the terminal as the last program left it.
func (a *App) userScreen() {
	a.lend(Job{Dir: a.root})
}

// lend gives the terminal to a job and redraws everything when it is back.
func (a *App) lend(job Job) {
	var failed error
	err := a.deps.Terminal.Lend(func() {
		failed = a.deps.Runner.Foreground(a.ctx, job)
	})
	a.shown = nil
	if err == nil {
		err = failed
	}
	if err != nil {
		a.fail(err)
	}
}
