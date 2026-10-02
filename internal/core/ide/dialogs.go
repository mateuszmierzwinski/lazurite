package ide

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mateuszmierzwinski/lazurite/internal/core/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/core/ui"
)

// The dialogs: what each asks, and what is done with the answer.

// ask puts a dialog up; then is told how it was answered.
func (a *App) ask(d ui.Dialog, then func(ui.Outcome)) {
	a.dialog, a.answered = d, then
	a.popup = nil
}

// tell shows a box of text.
func (a *App) tell(title string, lines ...string) {
	a.ask(ui.NewMessage(title, lines), nil)
}

// prompt asks for one line and hands it on when it is given and not empty.
func (a *App) prompt(title, label, start string, then func(string)) {
	p := ui.NewPrompt(title, label, start)
	a.ask(p, func(o ui.Outcome) {
		if text := strings.TrimSpace(p.Input.String()); o == ui.Accepted && text != "" {
			then(text)
		}
	})
}

// parent is the row of a directory listing that goes up.
const parent = "../"

// openDialog chooses a file to open, starting where the text being edited
// lives.
func (a *App) openDialog() {
	a.browse(a.packageDir())
}

// browse lists a directory to choose from: a directory chosen is listed in
// its turn, a file is opened, and a name typed that matches nothing is a
// new file.
func (a *App) browse(dir string) {
	entries, err := a.deps.Files.List(dir)
	if err != nil {
		a.fail(err)
		return
	}
	p := ui.NewPicker("Open — "+ui.Clip(displayName(a.root, dir), 50), entryLines(entries))
	p.Free = true
	a.ask(p, func(o ui.Outcome) {
		if o == ui.Accepted {
			a.browsed(dir, p.Choice())
		}
	})
}

// browsed acts on a choice made in a directory listing.
func (a *App) browsed(dir, choice string) {
	path := filepath.Join(dir, choice)
	if filepath.IsAbs(choice) {
		path = filepath.Clean(choice)
	}
	if strings.HasSuffix(choice, "/") {
		a.browse(path)
		return
	}
	a.open(path)
}

// entryLines is a directory's entries as lines: the way up first, and each
// directory marked with a slash.
func entryLines(entries []Entry) []string {
	out := []string{parent}
	for _, e := range entries {
		if e.Dir {
			out = append(out, e.Name+"/")
			continue
		}
		out = append(out, e.Name)
	}
	return out
}

// projectDialog chooses among every source file of the project, whatever
// directory or package it is in.
func (a *App) projectDialog() {
	files, err := a.deps.Files.Sources(a.root)
	if err != nil {
		a.fail(err)
		return
	}
	p := ui.NewPicker("Open in project", files)
	a.ask(p, func(o ui.Outcome) {
		if o == ui.Accepted {
			a.open(filepath.Join(a.root, p.Choice()))
		}
	})
}

// windowDialog chooses among the texts that are open.
func (a *App) windowDialog() {
	if len(a.docs) == 0 {
		a.say("No files are open")
		return
	}
	lines := make([]string, len(a.docs))
	for i, d := range a.docs {
		lines[i] = strconv.Itoa(i+1) + "  " + d.name
		if d.changed() {
			lines[i] += " *"
		}
	}
	p := ui.NewPicker("Windows", lines)
	a.ask(p, func(o ui.Outcome) {
		number, _, _ := strings.Cut(p.Choice(), " ")
		if n, err := strconv.Atoi(number); o == ui.Accepted && err == nil {
			a.show(n - 1)
		}
	})
}

// findDialog asks what to find and finds it.
func (a *App) findDialog() {
	ed := a.editor()
	if ed == nil {
		return
	}
	start := a.find
	if sel := ed.SelectedText(); sel != "" && !strings.Contains(sel, "\n") {
		start = sel
	}
	a.prompt("Find", "Text to find", start, func(text string) {
		a.find = text
		a.findAgain()
	})
}

// findAgain finds the next occurrence of what was last looked for.
func (a *App) findAgain() {
	ed := a.editor()
	if ed == nil || a.find == "" {
		return
	}
	if !ed.Find(a.find) {
		a.say("Not found: " + oneLine(a.find))
	}
}

// replaceDialog asks what to replace with what, and replaces every
// occurrence as one step of undo.
func (a *App) replaceDialog() {
	if a.editor() == nil {
		return
	}
	a.prompt("Replace", "Text to find", a.find, func(from string) {
		a.find = from
		p := ui.NewPrompt("Replace", "Replace every \""+ui.Clip(from, 20)+"\" with", "")
		a.ask(p, func(o ui.Outcome) {
			if o == ui.Accepted {
				a.replaceAll(from, p.Input.String())
			}
		})
	})
}

// replaceAll replaces every occurrence of a text in the text being edited.
func (a *App) replaceAll(from, to string) {
	ed := a.editor()
	if ed == nil {
		return
	}
	old := ed.Buf.Text()
	n := strings.Count(old, from)
	if n == 0 {
		a.say("Not found: " + oneLine(from))
		return
	}
	caret := ed.Caret
	ed.Buf.SetText(strings.ReplaceAll(old, from, to))
	ed.Goto(caret)
	a.say("Replaced " + strconv.Itoa(n))
}

// gotoDialog asks for a line and goes to it.
func (a *App) gotoDialog() {
	ed := a.editor()
	if ed == nil {
		return
	}
	a.prompt("Go to line", "Line, or line:column", strconv.Itoa(ed.Caret.Line+1), func(typed string) {
		at, ok := lineAndColumn(typed)
		if !ok {
			a.say("Not a line number: " + oneLine(typed))
			return
		}
		ed.Goto(at)
	})
}

// argumentsDialog asks what the program is given when it is run.
func (a *App) argumentsDialog() {
	p := ui.NewPrompt("Arguments", "Arguments the program is run with", a.args)
	a.ask(p, func(o ui.Outcome) {
		if o == ui.Accepted {
			a.args = strings.TrimSpace(p.Input.String())
		}
	})
}

// targetDialog asks which directory holds the program Run runs — or,
// where Bazel builds the project, which target.
func (a *App) targetDialog() {
	if a.bazel() {
		a.targetsDialog()
		return
	}
	start := a.target
	if start == "" {
		start = a.packageDir()
	}
	a.prompt("Target", "Directory of the program (package main)", displayName(a.root, start), func(typed string) {
		a.target = a.absolute(typed)
		a.say("Target: " + displayName(a.root, a.target))
	})
}

// evaluateDialog asks for an expression and works it out in the resting
// program.
func (a *App) evaluateDialog() {
	a.prompt("Evaluate", "Expression", a.wordAtCaret(), a.evaluate)
}

// watchDialog asks for an expression to watch.
func (a *App) watchDialog() {
	a.prompt("Add watch", "Expression", a.wordAtCaret(), a.addWatch)
}

// wordAtCaret is the selection when it is on one line, else the word the
// caret is in: what an expression dialog starts with.
func (a *App) wordAtCaret() string {
	ed := a.editor()
	if ed == nil {
		return ""
	}
	if sel := ed.SelectedText(); sel != "" && !strings.Contains(sel, "\n") {
		return sel
	}
	line := ed.Buf.Line(ed.Caret.Line)
	from, to := min(ed.Caret.Col, len(line)), min(ed.Caret.Col, len(line))
	for from > 0 && identifier(line[from-1]) {
		from--
	}
	for to < len(line) && identifier(line[to]) {
		to++
	}
	return string(line[from:to])
}

// help shows the keys.
func (a *App) help() {
	a.tell("Keys", helpLines...)
}

// helpLines is the key reference.
var helpLines = []string{
	"Files      F3 open · Ctrl+P open in project · F2 save",
	"           Ctrl+N new · Alt+F3 close · Alt+X exit",
	"Windows    F6 next · Shift+F6 previous · Alt+1…9 · Alt+0 list",
	"           F5 zoom · Alt+M messages · Alt+O output · Alt+V debug",
	"Project    Alt+T tree (Enter opens, arrows fold, Esc back to the text)",
	"           File → New Go project · File → Open project",
	"Editing    Ctrl+Z undo · Ctrl+Y redo · Ctrl+X/C/V clipboard",
	"           Ctrl+K clean up and fix imports (also on every save)",
	"           Ctrl+Space complete · Shift+arrows select",
	"Search     Ctrl+F find · Ctrl+L again · Ctrl+G go to line",
	"           F12 definition · Ctrl+D describe symbol",
	"           Alt+F8 next problem · Alt+F7 previous",
	"Monorepo   Ctrl+E Bazel targets: type to search, Enter for run/test/build",
	"           in a Bazel workspace F9, Alt+F9, Shift+F9 and Ctrl+F9 use Bazel",
	"Build      F9 build all · Alt+F9 build package",
	"           Shift+F9 test package · Ctrl+F2 stop",
	"Run        Ctrl+F9 run · Alt+F5 user screen",
	"Debug      F4 debug / continue · F8 over · F7 into · Shift+F8 out",
	"           Ctrl+F8 breakpoint · Ctrl+F4 evaluate · Ctrl+F7 watch",
	"Menus      F10, or Alt and the highlighted letter",
	"",
	"A terminal that keeps a key for itself still has the command:",
	"every one of them is in the menus.",
}

// about says what this is.
func (a *App) about() {
	version := a.deps.Version
	if version == "" {
		version = "development build"
	}
	a.tell("About", Brand, "A console IDE for Go", "", version, "", "F10 opens the menus · F1 lists the keys")
}

// caretFoot is the caret's place as the window's frame shows it: its line
// and the character it is at, counted as a compiler counts them, so that a
// place in an error message and the place in the frame are the same
// numbers.
func caretFoot(ed *edit.Editor) string {
	return strconv.Itoa(ed.Caret.Line+1) + ":" + strconv.Itoa(ed.Caret.Col+1)
}
