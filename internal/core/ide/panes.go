package ide

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

// The bottom window: what is wrong (Messages), what was printed (Output),
// and where the debugged program rests (Debug).

// showPane shows a page of the bottom window, with or without the
// keyboard.
func (a *App) showPane(kind paneKind, focus bool) {
	a.pane.kind, a.pane.shown = kind, true
	if focus {
		a.focus = focusPane
	}
}

// paneKey gives a key to the bottom window and reports whether it was the
// window's.
func (a *App) paneKey(ev term.Event) bool {
	list := &a.pane.lists[a.pane.kind]
	switch {
	case list.Key(ev, max(a.layout().bottom.H-3, 1)):
	case ev.Key == term.KeyEnter:
		a.paneEnter()
	default:
		return false
	}
	return true
}

// paneEnter acts on the chosen row: goes to its place, or opens its
// variable.
func (a *App) paneEnter() {
	rows := a.pane.rows[a.pane.kind]
	at := a.pane.lists[a.pane.kind].Sel
	if at < 0 || at >= len(rows) {
		return
	}
	switch r := rows[at]; {
	case r.jumps:
		a.jump(r.place)
	case r.variable > 0:
		a.toggleVariable(r.variable - 1)
	}
}

// paneMouse is the mouse over the bottom window: a press chooses a row and
// takes the keyboard; the wheel scrolls.
func (a *App) paneMouse(ev term.Event, window term.Rect) {
	list := &a.pane.lists[a.pane.kind]
	switch ev.Button {
	case term.MouseLeft:
		a.focus = focusPane
		list.Click(ev.Y - window.Y - 1)
	case term.WheelUp:
		list.Move(-wheelLines)
	case term.WheelDown:
		list.Move(wheelLines)
	}
}

// setRows gives a page its rows.
func (a *App) setRows(kind paneKind, rows []row) {
	a.pane.rows[kind] = rows
	texts := make([]string, len(rows))
	for i, r := range rows {
		texts[i] = r.text
	}
	a.pane.lists[kind].Set(texts)
}

// clearOutput empties the Output page.
func (a *App) clearOutput() {
	a.setRows(paneOutput, nil)
}

// print adds a line to the Output page.
func (a *App) print(text string) {
	a.printFrom("", text)
}

// printFrom adds a line of a job's output. A line that names a place in a
// file — a compiler's error, a failed test — becomes a row that goes
// there; the place is read relative to where the job ran.
func (a *App) printFrom(dir, text string) {
	r := row{text: strings.TrimRight(expandTabs(text), "\r")}
	if file, line, col, ok := parsePlace(text); ok && dir != "" {
		if !filepath.IsAbs(file) {
			file = filepath.Join(dir, file)
		}
		r.place, r.jumps = Place{Path: file, At: posAtLine(line - 1)}, true
		r.place.At.Col = max(col-1, 0)
	}
	rows := append(a.pane.rows[paneOutput], r)
	if len(rows) > maxOutputRows {
		rows = rows[len(rows)-maxOutputRows:]
	}
	follow := a.focus != focusPane || a.pane.kind != paneOutput
	a.setRows(paneOutput, rows)
	if follow {
		a.pane.lists[paneOutput].Sel = len(rows) - 1
	}
}

// heard takes what the language server says is wrong with a file. It is
// called from the server's goroutine.
func (a *App) heard(path string, problems []Problem) {
	a.post(func() {
		a.warmed()
		if len(problems) == 0 {
			delete(a.problems, path)
		} else {
			a.problems[path] = problems
		}
		a.rebuildMessages()
	})
}

// rebuildMessages lists every problem, by file and line.
func (a *App) rebuildMessages() {
	paths := make([]string, 0, len(a.problems))
	for path := range a.problems {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var rows []row
	for _, path := range paths {
		for _, p := range a.problems[path] {
			if len(rows) < maxProblemRows {
				rows = append(rows, a.problemRow(path, p))
			}
		}
	}
	a.setRows(paneMessages, rows)
}

// problemRow is one problem as a row that goes to it.
func (a *App) problemRow(path string, p Problem) row {
	kind := "warning"
	if p.Severe {
		kind = "error"
	}
	text := displayName(a.root, path) + ":" + strconv.Itoa(p.From.Line+1) + ":" +
		strconv.Itoa(p.From.Col+1) + ": " + kind + ": " + oneLine(p.Message)
	return row{text: text, place: Place{Path: path, At: p.From}, jumps: true}
}

// problemStep goes to the next or the previous place the bottom window
// names: a problem when there are any, else a place in the output.
func (a *App) problemStep(by int) {
	kind := paneMessages
	if len(a.pane.rows[paneMessages]) == 0 {
		kind = paneOutput
	}
	rows, list := a.pane.rows[kind], &a.pane.lists[kind]
	at := list.Sel
	for range rows {
		at = (at + by + len(rows)) % len(rows)
		if rows[at].jumps {
			list.Sel = at
			a.showPane(kind, false)
			a.jump(rows[at].place)
			a.say(rows[at].text)
			return
		}
	}
	a.say("No problems")
}

// rebuildDebug lists the variables, the watches and the call stack — in
// that order, because the window is short and the stack is what is looked
// at least.
func (a *App) rebuildDebug() {
	var rows []row
	if len(a.dbg.vars) > 0 {
		rows = append(rows, row{text: "Variables"})
	}
	for i, v := range a.dbg.vars {
		rows = append(rows, row{text: variableText(v), variable: i + 1})
	}
	if a.dbg.watches.Len() > 0 {
		rows = append(rows, row{text: "Watches"})
	}
	for _, w := range a.dbg.watches.List() {
		rows = append(rows, row{text: watchText(w)})
	}
	if len(a.dbg.frames) > 0 {
		rows = append(rows, row{text: "Call stack"})
	}
	for _, f := range a.dbg.frames {
		rows = append(rows, frameRow(f))
	}
	if len(rows) == 0 {
		rows = []row{{text: "Nothing is being debugged. F4 starts the program; Ctrl+F8 sets a breakpoint."}}
	}
	a.setRows(paneDebug, rows)
}

// frameRow is one frame of the call stack as a row that goes to it.
func frameRow(f debug.Frame) row {
	if f.File == "" {
		return row{text: "  " + f.Name}
	}
	text := "  " + f.Name + "   " + filepath.Base(f.File) + ":" + strconv.Itoa(f.Line)
	return row{text: text, place: Place{Path: f.File, At: posAtLine(f.Line - 1)}, jumps: true}
}

// variableText is one variable as a line: indented by how deep it is, with
// a mark when it can be opened.
func variableText(v variable) string {
	mark := "  "
	switch {
	case v.open:
		mark = "- "
	case v.v.Children > 0:
		mark = "+ "
	}
	return strings.Repeat("  ", v.depth+1) + mark + v.v.Name + " = " + valueText(v.v)
}

// valueText is a value with its type.
func valueText(v debug.Variable) string {
	if v.Type == "" {
		return oneLine(v.Value)
	}
	return oneLine(v.Value) + "  (" + v.Type + ")"
}

// watchText is one watched expression as a line.
func watchText(w debug.Watch) string {
	switch {
	case w.Err != "":
		return "    " + w.Expression + " : " + w.Err
	case !w.Fresh:
		return "    " + w.Expression + " = …"
	}
	return "    " + w.Expression + " = " + valueText(w.Value)
}

// closeVariable removes the lines under a variable: everything after it
// that is nested deeper.
func closeVariable(vars []variable, at int) []variable {
	end := at + 1
	for end < len(vars) && vars[end].depth > vars[at].depth {
		end++
	}
	return append(vars[:at+1], vars[end:]...)
}

// insertChildren puts a variable's children under it and marks it open. No
// more lines are added than the page shows.
func insertChildren(vars []variable, at int, children []debug.Variable) []variable {
	vars[at].open = true
	room := max(maxVariables-len(vars), 0)
	if len(children) > room {
		children = children[:room]
	}
	under := make([]variable, len(children))
	for i, c := range children {
		under[i] = variable{v: c, depth: vars[at].depth + 1}
	}
	return append(vars[:at+1], append(under, vars[at+1:]...)...)
}
