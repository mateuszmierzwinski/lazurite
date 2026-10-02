package ide

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mateuszmierzwinski/lazurite/internal/core/ui"
	"github.com/mateuszmierzwinski/lazurite/internal/monokit/bazeltarget"
	"github.com/mateuszmierzwinski/lazurite/internal/monokit/buildkind"
	"github.com/mateuszmierzwinski/lazurite/internal/monokit/gowork"
)

// Working inside a monorepo: finding out what builds the project and how it
// sits in the workspaces around it, building and running through Bazel
// where Bazel is the authority, finding a target by typing as Basalt does,
// and the go.work above a module that does not list it.

// workFile is the file that makes a directory a Go workspace; notBazel is
// what is said when a Bazel command is asked for outside a Bazel workspace.
const (
	workFile = "go.work"
	notBazel = "This project is not in a Bazel workspace"
)

// The limits of what Bazel is believed about.
const (
	// maxTargets is how many targets of a workspace are kept.
	maxTargets = 20000
	// maxQueryBytes is how much of a query's output is read.
	maxQueryBytes = 8 << 20
)

// survey finds out where the IDE is: what the project is built with, the
// Bazel workspace and the go.work around it, and whether that go.work
// lists it. It is done each time the IDE enters a project, before the
// language server is started there — a module its go.work does not list is
// refused by the whole Go toolchain, and the server must be started
// knowing.
func (a *App) survey() {
	p := project{kind: buildkind.Of(a.names(a.root))}
	p.workspace = a.above(a.root, bazeltarget.Markers())
	if p.workspace == "" && p.kind == buildkind.Bazel {
		p.kind = buildkind.Go
	}
	if rel, err := filepath.Rel(p.workspace, a.root); p.workspace != "" && err == nil && rel != "." {
		p.home = filepath.ToSlash(rel)
	}
	p.work = a.above(a.root, []string{workFile})
	p.detached = gowork.Detached(gowork.Env{Root: a.root, GOWORK: a.deps.Gowork}, a.look())
	a.proj, a.targets, a.listed, a.runTarget = p, nil, false, ""
	if a.deps.Detach != nil {
		a.deps.Detach(p.detached)
	}
}

// names is the names of a directory's entries.
func (a *App) names(dir string) []string {
	entries, err := a.deps.Files.List(dir)
	if err != nil {
		return nil
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

// above is the nearest directory, at or above one, that holds a file of
// one of some names; empty when none does.
func (a *App) above(dir string, names []string) string {
	for at := filepath.Clean(dir); ; at = filepath.Dir(at) {
		for _, name := range names {
			if a.exists(filepath.Join(at, name)) {
				return at
			}
		}
		if at == filepath.Dir(at) {
			return ""
		}
	}
}

// exists reports whether a path is there at all.
func (a *App) exists(path string) bool {
	_, err := a.deps.Files.Read(path)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

// look is the disk as the go.work rule asks about it.
func (a *App) look() gowork.Look {
	return gowork.Look{
		ReadFile: func(path string) ([]byte, bool) {
			text, err := a.deps.Files.Read(path)
			return []byte(text), err == nil
		},
		Exists: a.exists,
	}
}

// bazel reports whether Bazel builds this project.
func (a *App) bazel() bool {
	return a.proj.kind == buildkind.Bazel && a.proj.workspace != ""
}

// standing is the project's standing in a few words: what builds it, and
// whether its go.work lists it.
func (a *App) standing() string {
	out := "Go module"
	switch {
	case a.bazel():
		out = "Bazel " + bazeltarget.PackageLabel(a.proj.home)
	case a.proj.kind == buildkind.Unknown:
		out = "no go.mod"
	}
	if a.proj.detached {
		out += " · outside go.work (built standalone)"
	}
	return out
}

// pattern is the Bazel pattern a build or a test is about: everything in
// the project, or the one package of the text being edited.
func (a *App) pattern(whole bool) string {
	if whole {
		return bazeltarget.Everything(bazeltarget.PackageLabel(a.proj.home))
	}
	rel, err := filepath.Rel(a.proj.workspace, a.packageDir())
	if err != nil || strings.HasPrefix(rel, "..") {
		return bazeltarget.Everything(bazeltarget.PackageLabel(a.proj.home))
	}
	return bazeltarget.AllIn(bazeltarget.PackageLabel(filepath.ToSlash(rel)))
}

// bazelRun is the job that runs a target: the label one argument, then
// what the user wants the program itself given.
func (a *App) bazelRun(label string) Job {
	args := []string{"run", label}
	if extra := strings.Fields(a.args); len(extra) > 0 {
		args = append(append(args, "--"), extra...)
	}
	return Job{Tool: ToolBazel, Dir: a.proj.workspace, Args: args}
}

// targetsDialog finds a Bazel target by typing, as Basalt does: the
// workspace is asked what it holds — once, and again when asked to — and
// the answer is searched.
func (a *App) targetsDialog() {
	switch {
	case !a.bazel():
		a.say(notBazel)
	case !a.listed:
		a.listTargets()
	default:
		a.showTargets()
	}
}

// refreshTargets asks the workspace again.
func (a *App) refreshTargets() {
	if !a.bazel() {
		a.say(notBazel)
		return
	}
	a.listTargets()
}

// listTargets asks Bazel what the workspace holds and then shows it.
func (a *App) listTargets() {
	if a.stop != nil {
		a.fail(ErrBusy)
		return
	}
	a.say("Asking Bazel what the workspace holds… (Ctrl+F2 stops)")
	var printed strings.Builder
	job := Job{Tool: ToolBazel, Dir: a.proj.workspace, Args: bazeltarget.QueryArgs()}
	a.stop = a.deps.Runner.Capture(a.ctx, job,
		func(line string) {
			a.post(func() {
				if printed.Len() < maxQueryBytes {
					printed.WriteString(line + "\n")
				}
			})
		},
		func(err error) { a.post(func() { a.targetsListed(printed.String(), err) }) })
}

// targetsListed takes Bazel's answer. A query that failed and still named
// targets is an answer — Bazel keeps going past a package it cannot load —
// and one that named none is a failure.
func (a *App) targetsListed(printed string, err error) {
	a.stop = nil
	targets := bazeltarget.Parse(printed)
	if len(targets) == 0 {
		a.say("Bazel named no targets: " + lastLine(printed, err))
		return
	}
	if len(targets) > maxTargets {
		targets = targets[:maxTargets]
	}
	a.targets, a.listed = targets, true
	a.say(strconv.Itoa(len(targets)) + " targets")
	a.showTargets()
}

// lastLine is the last thing a failed command said, or how it failed.
func lastLine(printed string, err error) string {
	lines := strings.Split(strings.TrimSpace(printed), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return oneLine(last)
	}
	if err != nil {
		return oneLine(err.Error())
	}
	return "it printed nothing"
}

// showTargets puts the search up: the project's own targets first, what
// can be run or tested before what can only be built, narrowed by what is
// typed — a plain name, or a pattern.
func (a *App) showTargets() {
	rows := map[string]bazeltarget.Target{}
	match := func(query string) []string {
		found, err := bazeltarget.Find(a.targets, query, a.proj.home)
		if err != nil {
			return nil
		}
		lines := make([]string, len(found))
		for i, t := range found {
			lines[i] = targetLine(t)
			rows[lines[i]] = t
		}
		return lines
	}
	p := ui.NewPicker("Bazel targets — type a name or a pattern", match(""))
	p.Match = match
	a.ask(p, func(o ui.Outcome) {
		if t, ok := rows[p.Choice()]; ok && o == ui.Accepted {
			a.targetActions(t)
		}
	})
}

// targetMarks is the mark each kind of target is listed with.
var targetMarks = map[bazeltarget.Verb]string{
	bazeltarget.VerbRun:   "▶ ",
	bazeltarget.VerbTest:  "◆ ",
	bazeltarget.VerbBuild: "· ",
}

// targetLine is one target as a line of the search: what doing it means,
// its label, its kind.
func targetLine(t bazeltarget.Target) string {
	return targetMarks[bazeltarget.VerbOf(t)] + t.Label + "  " + string(t.Kind)
}

// targetAction is one thing that can be done with a target.
type targetAction struct {
	name string
	do   func(a *App, t bazeltarget.Target)
}

// actions is what can be done with a target, in the order offered after
// the one the target's kind means.
var actions = []targetAction{
	{name: "Run", do: (*App).runTargetNow},
	{name: "Test", do: (*App).testTarget},
	{name: "Build", do: (*App).buildTarget},
	{name: "Use for Ctrl+F9 (the run target)", do: (*App).keepTarget},
	{name: "Open its BUILD file", do: (*App).openBuildFile},
}

// natural is the name of the action a target's kind means: a binary is
// run, a test is tested, the rest is built.
var natural = map[bazeltarget.Verb]string{
	bazeltarget.VerbRun: "Run", bazeltarget.VerbTest: "Test", bazeltarget.VerbBuild: "Build",
}

// targetActions offers what can be done with a chosen target, the action
// its kind means first.
func (a *App) targetActions(t bazeltarget.Target) {
	first := natural[bazeltarget.VerbOf(t)]
	lines := []string{first}
	for _, action := range actions {
		if action.name != first {
			lines = append(lines, action.name)
		}
	}
	p := ui.NewPicker(ui.Clip(t.Label, 56), lines)
	a.ask(p, func(o ui.Outcome) {
		if o == ui.Accepted {
			a.act(p.Choice(), t)
		}
	})
}

// act does the action of a name with a target.
func (a *App) act(name string, t bazeltarget.Target) {
	for _, action := range actions {
		if action.name == name {
			action.do(a, t)
			return
		}
	}
}

// runTargetNow runs a target on the terminal, and remembers it as what Run
// runs.
func (a *App) runTargetNow(t bazeltarget.Target) {
	a.runTarget = t.Label
	if a.saveAll() {
		a.lend(a.bazelRun(t.Label))
	}
}

// testTarget tests one target.
func (a *App) testTarget(t bazeltarget.Target) {
	a.capture("Test", Job{Tool: ToolBazel, Dir: a.proj.workspace, Args: []string{"test", t.Label}})
}

// buildTarget builds one target.
func (a *App) buildTarget(t bazeltarget.Target) {
	a.capture("Build", Job{Tool: ToolBazel, Dir: a.proj.workspace, Args: []string{"build", t.Label}})
}

// keepTarget makes a target what Run runs.
func (a *App) keepTarget(t bazeltarget.Target) {
	a.runTarget = t.Label
	a.say("Ctrl+F9 runs " + t.Label)
}

// buildNames is what a package's BUILD file may be called.
var buildNames = []string{"BUILD", "BUILD.bazel"}

// openBuildFile opens the BUILD file that declares a target.
func (a *App) openBuildFile(t bazeltarget.Target) {
	dir := filepath.Join(a.proj.workspace, filepath.FromSlash(strings.TrimPrefix(t.Package, "//")))
	for _, name := range buildNames {
		if path := filepath.Join(dir, name); a.exists(path) {
			a.open(path)
			return
		}
	}
	a.say("No BUILD file in " + displayName(a.root, dir))
}

// addToWork lists the project in the go.work above it, so that it is built
// with the workspace's modules rather than standalone.
func (a *App) addToWork() {
	if !a.proj.detached || a.proj.work == "" {
		a.say("There is no go.work above this project that leaves it out")
		return
	}
	path := filepath.Join(a.proj.work, workFile)
	body, err := a.deps.Files.Read(path)
	rel, relErr := filepath.Rel(a.proj.work, a.root)
	if err != nil || relErr != nil {
		a.say("go.work could not be read")
		return
	}
	if err := a.deps.Files.Write(path, withUse(body, "./"+filepath.ToSlash(rel))); err != nil {
		a.fail(err)
		return
	}
	a.survey()
	a.reroot()
	a.say("Added to " + displayName(a.root, path) + " — " + a.standing())
}

// offerWork asks whether a project its go.work leaves out should be added
// to it; a No leaves it built standalone.
func (a *App) offerWork() {
	if !a.proj.detached || a.dialog != nil {
		return
	}
	a.ask(ui.NewConfirm("go.work", "Add this project to the go.work above it?"), func(o ui.Outcome) {
		if o == ui.Accepted {
			a.addToWork()
		}
	})
}

// withUse is a go.work's text with one more module directory: inside its
// `use (` block when it has one, as a line of its own when it has not.
func withUse(body, entry string) string {
	lines := strings.Split(body, "\n")
	open := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "use (" || trimmed == "use(" {
			open = i
		}
		if open >= 0 && trimmed == ")" {
			grown := append(append(append([]string{}, lines[:i]...), "\t"+entry), lines[i:]...)
			return strings.Join(grown, "\n")
		}
	}
	return strings.TrimRight(body, "\n") + "\n\nuse " + entry + "\n"
}

// targetName is a directory's name made fit to be a Bazel target's: the
// characters a label may hold, and something rather than nothing.
func targetName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if r == '_' || r == '-' || r == '.' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			sb.WriteRune(r)
		}
	}
	if sb.Len() == 0 {
		return "app"
	}
	return sb.String()
}

// buildFile is the BUILD file of a new program in a Bazel workspace: a
// library of its sources and the binary made of it, as the workspace's
// other programs are declared.
func buildFile(name, module string) string {
	target := targetName(name)
	return "load(\"@io_bazel_rules_go//go:def.bzl\", \"go_binary\", \"go_library\")\n\n" +
		"go_binary(\n    name = " + strconv.Quote(target) + ",\n    embed = [\":" + target + "_lib\"],\n" +
		"    visibility = [\"//visibility:public\"],\n)\n\n" +
		"go_library(\n    name = " + strconv.Quote(target+"_lib") + ",\n    srcs = [\"main.go\"],\n" +
		"    importpath = " + strconv.Quote(module) + ",\n    visibility = [\"//visibility:private\"],\n)\n"
}
