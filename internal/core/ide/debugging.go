package ide

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mateuszmierzwinski/lazurite/internal/core/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
)

// Debugging: breakpoints, starting and stepping a program, and showing
// where it rests and what its variables hold.
//
// Every call into the debugger is made off the loop (spawn), because each
// may wait for the program; what it answers comes back through post.

// watcher is who the debugger reports to. It posts and returns, which is
// the contract the debugger's port asks of a watcher.
type watcher struct{ a *App }

// Changed takes news of the session.
func (w watcher) Changed(s debug.Session) {
	w.a.post(func() { w.a.sessionChanged(s) })
}

// Output takes what the program printed.
func (w watcher) Output(text string) {
	w.a.post(func() { w.a.print(strings.TrimRight(text, "\r\n")) })
}

// busy reports whether a program is being started or is running: neither
// can be stepped or started again.
func (a *App) busy() bool {
	st := a.dbg.session.State
	return st == debug.Starting || st == debug.Running
}

// debug starts the program under the debugger, or lets a resting one go on.
func (a *App) debug() {
	switch {
	case a.debugger == nil:
		a.say("This build has no debugger")
	case a.dbg.session.AtRest():
		a.resume(debug.Debugger.Continue)
	case a.busy():
		a.say("The program is running — Ctrl+F2 stops it")
	default:
		a.launch(false)
	}
}

// step resumes a resting program by one step; with no program started it
// starts one and stops at the first line of main.
func (a *App) step(how func(debug.Debugger, context.Context) error) {
	switch {
	case a.debugger == nil:
		a.say("This build has no debugger")
	case a.dbg.session.AtRest():
		a.resume(how)
	case a.busy():
		a.say("The program is running — Ctrl+F2 stops it")
	default:
		a.launch(true)
	}
}

// resume lets a resting program go on in one of the four ways.
//
// The program is taken to be running from this moment, not from when the
// debugger says so: the debugger is asked off the loop, and a second key
// pressed before it answered would otherwise be acted on as if the program
// still rested where the screen shows it.
func (a *App) resume(how func(debug.Debugger, context.Context) error) {
	a.dbg.session = debug.Session{State: debug.Running}
	a.running()
	a.rebuildDebug()
	a.spawn(func() {
		if err := how(a.debugger, a.ctx); err != nil {
			a.post(func() { a.fail(err) })
		}
	})
}

// launch starts the program under the debugger: held at its entry, given
// the breakpoints, then let go. atMain adds a breakpoint on the first line
// of main that lasts until it is reached.
func (a *App) launch(atMain bool) {
	dir, ok := a.targetDir()
	if !ok {
		a.fail(ErrNoTarget)
		return
	}
	if !a.saveAll() {
		return
	}
	if place, found := a.mainPlace(dir); atMain && found {
		a.dbg.once, a.dbg.onceSet = place, true
	}
	a.dbg.session = debug.Session{State: debug.Starting}
	a.clearOutput()
	a.print("Debugging " + displayName(a.root, dir))
	a.showPane(paneOutput, false)
	a.say("Building for the debugger…")
	points, args := a.allPoints(), a.runArgs()
	a.dbg.stale = map[string]bool{}
	a.spawn(func() { a.start(dir, args, points) })
}

// start is the debugger's own part of a launch. It runs off the loop.
func (a *App) start(dir string, args []string, points map[string][]debug.Breakpoint) {
	if err := a.debugger.Launch(a.ctx, "go", args, dir); err != nil {
		a.post(func() { a.sessionChanged(debug.Session{State: debug.Ended, Err: err}) })
		return
	}
	for file, list := range points {
		a.report(a.debugger.SetBreakpoints(a.ctx, file, list))
	}
	if err := a.debugger.Continue(a.ctx); err != nil {
		a.post(func() { a.fail(err) })
	}
}

// report says which breakpoints the debugger could not set. It may be
// called from any goroutine.
func (a *App) report(set []debug.Breakpoint, err error) {
	for _, b := range set {
		if !b.Verified {
			text := "Breakpoint not set at " + filepath.Base(b.File) + ":" + strconv.Itoa(b.Line) + ": " + b.Message
			a.post(func() { a.print(text) })
		}
	}
	if err != nil {
		a.post(func() { a.print("Breakpoints: " + oneLine(err.Error())) })
	}
}

// allPoints is every breakpoint, by file, as the debugger wants them: lines
// counted from one. It includes the one that stops at main.
func (a *App) allPoints() map[string][]debug.Breakpoint {
	out := map[string][]debug.Breakpoint{}
	for file := range a.dbg.breaks {
		if list := a.pointsIn(file); len(list) > 0 {
			out[file] = list
		}
	}
	if a.dbg.onceSet {
		out[a.dbg.once.Path] = a.pointsIn(a.dbg.once.Path)
	}
	return out
}

// pointsIn is one file's breakpoints, in line order.
func (a *App) pointsIn(file string) []debug.Breakpoint {
	lines := map[int]bool{}
	for line := range a.dbg.breaks[file] {
		lines[line] = true
	}
	if a.dbg.onceSet && a.dbg.once.Path == file {
		lines[a.dbg.once.At.Line] = true
	}
	sorted := make([]int, 0, len(lines))
	for line := range lines {
		sorted = append(sorted, line)
	}
	sort.Ints(sorted)
	out := make([]debug.Breakpoint, 0, len(sorted))
	for _, line := range sorted {
		out = append(out, debug.Breakpoint{File: file, Line: line + 1})
	}
	return out
}

// sessionChanged takes news of the session from the debugger. News that a
// program ended, about one already known to be gone, is the echo of the
// IDE's own tidying up and is dropped.
func (a *App) sessionChanged(s debug.Session) {
	if s.State == debug.Ended && a.dbg.session.State == debug.Off {
		return
	}
	a.dbg.session = s
	switch s.State {
	case debug.Stopped:
		a.rested()
	case debug.Ended:
		a.ended(s.Err)
	default:
		a.running()
	}
	a.rebuildDebug()
}

// running is the program going: it rests nowhere, and what was read from
// it when it last stopped is no longer what it holds.
func (a *App) running() {
	a.dbg.at, a.dbg.frames, a.dbg.vars = false, nil, nil
	a.dbg.watches.Stale()
}

// rested is the program stopping: ask where, and show it.
func (a *App) rested() {
	a.say("Stopped")
	a.spawn(func() {
		frames, err := a.debugger.Frames(a.ctx)
		a.post(func() { a.showFrames(frames, err) })
	})
}

// showFrames shows where the program rests: the caret goes to the line,
// the variables there are fetched, and breakpoints changed while it ran
// are passed on.
func (a *App) showFrames(frames []debug.Frame, err error) {
	if err != nil || !a.dbg.session.AtRest() {
		return
	}
	a.dbg.frames, a.dbg.vars = frames, nil
	a.dropOnce()
	a.sendBreaks()
	for _, f := range frames {
		if f.File != "" {
			a.dbg.resume, a.dbg.at = Place{Path: f.File, At: posAtLine(f.Line - 1)}, true
			a.jump(a.dbg.resume)
			break
		}
	}
	a.fetchVariables()
	a.refreshWatches()
	a.showPane(paneDebug, false)
	a.rebuildDebug()
}

// dropOnce forgets the breakpoint that only stopped the program at main.
func (a *App) dropOnce() {
	if a.dbg.onceSet {
		a.dbg.onceSet = false
		a.dbg.stale[a.dbg.once.Path] = true
	}
}

// fetchVariables asks for the variables of the frame the program rests in.
func (a *App) fetchVariables() {
	if len(a.dbg.frames) == 0 {
		return
	}
	scope := a.dbg.frames[0].Scope
	a.spawn(func() {
		vars, err := a.debugger.Variables(a.ctx, scope)
		a.post(func() { a.showVariables(vars, err) })
	})
}

// showVariables shows a frame's variables.
func (a *App) showVariables(vars []debug.Variable, err error) {
	if err != nil {
		return
	}
	a.dbg.vars = a.dbg.vars[:0]
	for _, v := range vars {
		a.dbg.vars = append(a.dbg.vars, variable{v: v})
	}
	a.rebuildDebug()
}

// toggleVariable opens a variable to show what it holds, or closes it.
func (a *App) toggleVariable(i int) {
	if i < 0 || i >= len(a.dbg.vars) || a.dbg.vars[i].v.Children == 0 {
		return
	}
	v := &a.dbg.vars[i]
	if v.open {
		v.open = false
		a.dbg.vars = closeVariable(a.dbg.vars, i)
		a.rebuildDebug()
		return
	}
	ref, name := v.v.Ref, v.v.Name
	a.spawn(func() {
		children, err := a.debugger.Variables(a.ctx, ref)
		a.post(func() { a.openVariable(name, ref, children, err) })
	})
}

// openVariable shows the children of a variable under it. The variable is
// found again by what it is, because the list may have changed while the
// debugger was asked.
func (a *App) openVariable(name string, ref int, children []debug.Variable, err error) {
	if err != nil {
		return
	}
	for i, v := range a.dbg.vars {
		if v.v.Ref == ref && v.v.Name == name && !v.open {
			a.dbg.vars = insertChildren(a.dbg.vars, i, children)
			break
		}
	}
	a.rebuildDebug()
}

// ended is the program gone: nothing rests anywhere any more.
func (a *App) ended(err error) {
	a.dbg.at, a.dbg.frames, a.dbg.vars, a.dbg.onceSet = false, nil, nil, false
	a.dbg.session = debug.Session{State: debug.Off}
	if err != nil {
		a.print("✗ " + oneLine(err.Error()))
		a.fail(err)
	} else {
		a.print("Program ended")
		a.say("Program ended")
	}
	a.spawn(func() { _ = a.debugger.Stop(a.ctx) })
}

// stopDebugging ends the debugged program.
func (a *App) stopDebugging() {
	if a.debugger == nil || a.dbg.session.State == debug.Off {
		return
	}
	a.say("Stopping…")
	a.spawn(func() { _ = a.debugger.Stop(a.ctx) })
}

// toggleBreakpoint sets or removes a breakpoint on the caret's line.
func (a *App) toggleBreakpoint() {
	d := a.doc()
	if d == nil {
		return
	}
	if d.unnamed {
		a.fail(ErrUnnamed)
		return
	}
	lines := a.dbg.breaks[d.path]
	if lines == nil {
		lines = map[int]bool{}
		a.dbg.breaks[d.path] = lines
	}
	if line := d.ed.Caret.Line; lines[line] {
		delete(lines, line)
	} else {
		lines[line] = true
	}
	a.dbg.stale[d.path] = true
	a.sendBreaks()
}

// clearBreakpoints removes every breakpoint.
func (a *App) clearBreakpoints() {
	for file := range a.dbg.breaks {
		a.dbg.stale[file] = true
	}
	a.dbg.breaks = map[string]map[int]bool{}
	a.sendBreaks()
	a.say("Breakpoints cleared")
}

// sendBreaks tells the debugger of the breakpoints changed since it was
// last told. Only a resting program can be told: the debugger takes
// breakpoints between runs, and asking it while the program runs would
// wait for the program. Changes made meanwhile are kept and sent at the
// next stop.
func (a *App) sendBreaks() {
	if !a.dbg.session.AtRest() || len(a.dbg.stale) == 0 {
		return
	}
	points := map[string][]debug.Breakpoint{}
	for file := range a.dbg.stale {
		points[file] = a.pointsIn(file)
	}
	a.dbg.stale = map[string]bool{}
	a.spawn(func() {
		for file, list := range points {
			a.report(a.debugger.SetBreakpoints(a.ctx, file, list))
		}
	})
}

// frameID is the frame expressions are evaluated in: where the program
// rests.
func (a *App) frameID() int {
	if len(a.dbg.frames) == 0 {
		return 0
	}
	return a.dbg.frames[0].ID
}

// evaluate works an expression out in the resting program and shows it.
func (a *App) evaluate(expression string) {
	if !a.dbg.session.AtRest() {
		a.say("The program is not stopped")
		return
	}
	frame := a.frameID()
	a.spawn(func() {
		v, err := a.debugger.Evaluate(a.ctx, expression, frame)
		a.post(func() { a.evaluated(expression, v, err) })
	})
}

// evaluated shows what an expression came to.
func (a *App) evaluated(expression string, v debug.Variable, err error) {
	if err != nil {
		a.tell("Evaluate", expression, "", oneLine(err.Error()))
		return
	}
	a.tell("Evaluate", append([]string{expression, ""}, wrap("= "+valueText(v), describeWidth)...)...)
}

// addWatch keeps an expression to be worked out at every stop.
func (a *App) addWatch(expression string) {
	if !a.dbg.watches.Add(expression) {
		a.say("Not added: empty, already watched, or too many")
		return
	}
	a.refreshWatches()
	a.showPane(paneDebug, false)
	a.rebuildDebug()
}

// clearWatches forgets every watched expression.
func (a *App) clearWatches() {
	a.dbg.watches = debug.Watches{}
	a.rebuildDebug()
}

// refreshWatches works every watched expression out again.
func (a *App) refreshWatches() {
	a.dbg.watches.Stale()
	if !a.dbg.session.AtRest() {
		return
	}
	frame := a.frameID()
	for _, expression := range a.dbg.watches.Expressions() {
		a.spawn(func() {
			v, err := a.debugger.Evaluate(a.ctx, expression, frame)
			a.post(func() { a.watched(expression, v, err) })
		})
	}
}

// watched takes what a watched expression came to.
func (a *App) watched(expression string, v debug.Variable, err error) {
	text := ""
	if err != nil {
		text = oneLine(err.Error())
	}
	a.dbg.watches.Answer(expression, v, text)
	a.rebuildDebug()
}

// breaksIn is the lines of a file that carry a breakpoint.
func (a *App) breaksIn(path string) map[int]bool {
	return a.dbg.breaks[path]
}

// resumeLine is the line of a file the program rests on, or -1.
func (a *App) resumeLine(path string) int {
	if a.dbg.at && a.dbg.resume.Path == path {
		return a.dbg.resume.At.Line
	}
	return -1
}

// posAtLine is the start of a line.
func posAtLine(line int) edit.Pos {
	return edit.Pos{Line: max(line, 0)}
}
