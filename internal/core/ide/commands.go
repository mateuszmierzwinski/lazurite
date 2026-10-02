package ide

import (
	"context"

	"github.com/mateuszmierzwinski/lazurite/internal/core/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
	"github.com/mateuszmierzwinski/lazurite/internal/core/ui"
	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
)

// Everything the IDE can be asked to do, by name. A menu item and a key
// both name a command; neither knows how it is done.

// caretMove moves the caret, growing the selection when keep is set.
type caretMove struct {
	move func(*edit.Editor, bool)
	keep bool
}

func (c caretMove) Run(a *App) {
	if ed := a.editor(); ed != nil {
		c.move(ed, c.keep)
	}
}

// lineMove moves the caret some lines, or some pages of the window.
type lineMove struct {
	lines, pages int
	keep         bool
}

func (c lineMove) Run(a *App) {
	if ed := a.editor(); ed != nil {
		ed.Vertical(c.lines+c.pages*a.page(), c.keep)
	}
}

// textEdit changes the text in a way that needs nothing but the editor.
type textEdit struct{ change func(*edit.Editor) }

func (c textEdit) Run(a *App) {
	if ed := a.editor(); ed != nil {
		c.change(ed)
	}
}

// insertTab types a tab.
type insertTab struct{}

func (insertTab) Run(a *App) {
	if ed := a.editor(); ed != nil {
		ed.Type('\t')
	}
}

// stepCommand resumes a debugged program in one of the four ways.
type stepCommand struct {
	how func(debug.Debugger, context.Context) error
}

func (c stepCommand) Run(a *App) { a.step(c.how) }

// showPage shows one page of the bottom window and gives it the keyboard.
type showPage struct{ kind paneKind }

func (c showPage) Run(a *App) { a.showPane(c.kind, true) }

// showWindow switches to the text with a number.
type showWindow struct{ number int }

func (c showWindow) Run(a *App) { a.show(c.number - 1) }

// nextWindow switches to the text some places along.
type nextWindow struct{ by int }

func (c nextWindow) Run(a *App) {
	if len(a.docs) > 0 {
		a.show((a.at + c.by + len(a.docs)) % len(a.docs))
	}
}

// problemStep jumps to the next or the previous problem.
type problemStep struct{ by int }

func (c problemStep) Run(a *App) { a.problemStep(c.by) }

// goJob runs the Go tool on the project or on the package of the text
// being edited.
type goJob struct {
	title string
	args  []string
	// whole runs it at the project's root rather than in the package.
	whole bool
	// bazel is the Bazel subcommand that does the same where Bazel builds
	// the project; empty for a job only the Go tool does.
	bazel string
}

func (c goJob) Run(a *App) { a.goJob(c) }

// simple is a command that is one method of the IDE.
type simple struct{ do func(*App) }

func (c simple) Run(a *App) { c.do(a) }

// commands is every command, by name. It is filled once, in init.
var commands map[string]Command

// keymap is the command each chord runs. It is filled once, in init.
var keymap map[chord]string

func init() {
	commands = map[string]Command{}
	keymap = map[chord]string{}
	registerCaret()
	registerEditing()
	registerFiles()
	registerSearch()
	registerRunning()
	registerDebugging()
	registerWindows()
}

// bind names a command and gives it its chords.
func bind(name string, c Command, chords ...chord) {
	commands[name] = c
	for _, ch := range chords {
		keymap[ch] = name
	}
}

// plain is a key with nothing held; with adds modifiers; ctrl and alt are a
// letter with Ctrl or Alt.
func plain(k term.KeyCode) chord             { return chord{key: k} }
func with(k term.KeyCode, m term.Mods) chord { return chord{key: k, mods: m} }
func ctrl(r rune) chord                      { return chord{key: term.KeyRune, r: r, mods: term.Ctrl} }
func alt(r rune) chord                       { return chord{key: term.KeyRune, r: r, mods: term.Alt} }
func both(name string, k term.KeyCode, m term.Mods, move func(*edit.Editor, bool)) {
	bind(name, caretMove{move: move}, with(k, m))
	bind("select."+name, caretMove{move: move, keep: true}, with(k, m|term.Shift))
}

// registerCaret is the keys that move the caret; each with Shift selects.
func registerCaret() {
	both("left", term.KeyLeft, 0, (*edit.Editor).Left)
	both("right", term.KeyRight, 0, (*edit.Editor).Right)
	both("home", term.KeyHome, 0, (*edit.Editor).Home)
	both("end", term.KeyEnd, 0, (*edit.Editor).End)
	both("word.left", term.KeyLeft, term.Ctrl, (*edit.Editor).WordLeft)
	both("word.right", term.KeyRight, term.Ctrl, (*edit.Editor).WordRight)
	both("text.start", term.KeyHome, term.Ctrl, (*edit.Editor).DocStart)
	both("text.end", term.KeyEnd, term.Ctrl, (*edit.Editor).DocEnd)
	for _, keep := range []bool{false, true} {
		mods, prefix := term.Mods(0), ""
		if keep {
			mods, prefix = term.Shift, "select."
		}
		bind(prefix+"up", lineMove{lines: -1, keep: keep}, with(term.KeyUp, mods))
		bind(prefix+"down", lineMove{lines: 1, keep: keep}, with(term.KeyDown, mods))
		bind(prefix+"page.up", lineMove{pages: -1, keep: keep}, with(term.KeyPgUp, mods))
		bind(prefix+"page.down", lineMove{pages: 1, keep: keep}, with(term.KeyPgDn, mods))
	}
}

// registerEditing is the keys that change the text.
func registerEditing() {
	bind("enter", simple{do: (*App).enter}, plain(term.KeyEnter))
	bind("tab", insertTab{}, plain(term.KeyTab))
	bind("backspace", textEdit{change: (*edit.Editor).Backspace}, plain(term.KeyBackspace))
	bind("delete", textEdit{change: (*edit.Editor).Delete}, plain(term.KeyDelete))
	bind("undo", textEdit{change: (*edit.Editor).Undo}, ctrl('z'))
	bind("redo", textEdit{change: (*edit.Editor).Redo}, ctrl('y'))
	bind("select.all", textEdit{change: (*edit.Editor).SelectAll}, ctrl('a'))
	bind("copy", simple{do: (*App).copy}, ctrl('c'), with(term.KeyInsert, term.Ctrl))
	bind("cut", simple{do: (*App).cut}, ctrl('x'), with(term.KeyDelete, term.Shift))
	bind("paste", simple{do: (*App).pasteClip}, ctrl('v'), with(term.KeyInsert, term.Shift))
	bind("cleanup", simple{do: (*App).cleanUp}, ctrl('k'))
	bind("complete", simple{do: (*App).completeNow}, ctrl(' '))
	bind("escape", simple{do: (*App).escape}, plain(term.KeyEsc))
}

// registerFiles is the File menu.
func registerFiles() {
	bind("new", simple{do: (*App).newDocument}, ctrl('n'))
	bind("open", simple{do: (*App).openDialog}, plain(term.KeyF3), ctrl('o'))
	bind("project", simple{do: (*App).projectDialog}, ctrl('p'))
	bind("save", simple{do: (*App).save}, plain(term.KeyF2), ctrl('s'))
	bind("save.as", simple{do: (*App).saveAs})
	bind("save.all", simple{do: (*App).saveEverything})
	bind("close", simple{do: (*App).closeDocument}, with(term.KeyF3, term.Alt), ctrl('w'))
	bind("quit", simple{do: (*App).leave}, alt('x'), ctrl('q'))
	bind("project.new", simple{do: (*App).newProjectDialog})
	bind("project.open", simple{do: (*App).openProjectDialog})
	bind("work.add", simple{do: (*App).addToWork})
}

// registerSearch is the Search menu.
func registerSearch() {
	bind("find", simple{do: (*App).findDialog}, ctrl('f'))
	bind("find.again", simple{do: (*App).findAgain}, ctrl('l'))
	bind("replace", simple{do: (*App).replaceDialog})
	bind("goto", simple{do: (*App).gotoDialog}, ctrl('g'))
	bind("define", simple{do: (*App).define}, plain(term.KeyF12))
	bind("describe", simple{do: (*App).describe}, ctrl('d'))
	bind("problem.next", problemStep{by: 1}, with(term.KeyF8, term.Alt))
	bind("problem.previous", problemStep{by: -1}, with(term.KeyF7, term.Alt))
}

// registerRunning is the Run and Build menus.
func registerRunning() {
	bind("run", simple{do: (*App).runProgram}, with(term.KeyF9, term.Ctrl), ctrl('r'))
	bind("arguments", simple{do: (*App).argumentsDialog})
	bind("target", simple{do: (*App).targetDialog})
	bind("user.screen", simple{do: (*App).userScreen}, with(term.KeyF5, term.Alt))
	bind("build", goJob{title: "Build", args: []string{"build", "./..."}, whole: true, bazel: "build"}, plain(term.KeyF9))
	bind("build.package", goJob{title: "Build package", args: []string{"build", "-o", discard, "."}, bazel: "build"}, with(term.KeyF9, term.Alt))
	bind("test.package", goJob{title: "Test package", args: []string{"test", "."}, bazel: "test"}, with(term.KeyF9, term.Shift), ctrl('t'))
	bind("test", goJob{title: "Test", args: []string{"test", "./..."}, whole: true, bazel: "test"})
	bind("targets", simple{do: (*App).targetsDialog}, ctrl('e'))
	bind("targets.refresh", simple{do: (*App).refreshTargets})
	bind("vet", goJob{title: "Vet", args: []string{"vet", "./..."}, whole: true})
	bind("stop", simple{do: (*App).stopEverything}, with(term.KeyF2, term.Ctrl))
}

// discard is where a build that is only a check writes its program.
const discard = "/dev/null"

// registerDebugging is the Debug menu.
func registerDebugging() {
	bind("debug", simple{do: (*App).debug}, plain(term.KeyF4))
	bind("step.over", stepCommand{how: debug.Debugger.Step}, plain(term.KeyF8))
	bind("step.into", stepCommand{how: debug.Debugger.StepIn}, plain(term.KeyF7))
	bind("step.out", stepCommand{how: debug.Debugger.StepOut}, with(term.KeyF8, term.Shift))
	bind("breakpoint", simple{do: (*App).toggleBreakpoint}, with(term.KeyF8, term.Ctrl), ctrl('b'))
	bind("breakpoints.clear", simple{do: (*App).clearBreakpoints})
	bind("evaluate", simple{do: (*App).evaluateDialog}, with(term.KeyF4, term.Ctrl))
	bind("watch", simple{do: (*App).watchDialog}, with(term.KeyF7, term.Ctrl))
	bind("watch.clear", simple{do: (*App).clearWatches})
}

// registerWindows is the Window and Help menus, and the menu bar itself.
func registerWindows() {
	bind("window.next", nextWindow{by: 1}, plain(term.KeyF6))
	bind("window.previous", nextWindow{by: -1}, with(term.KeyF6, term.Shift))
	bind("window.list", simple{do: (*App).windowDialog}, alt('0'))
	for n := 1; n <= 9; n++ {
		bind("window."+string(rune('0'+n)), showWindow{number: n}, alt(rune('0'+n)))
	}
	bind("zoom", simple{do: (*App).zoom}, plain(term.KeyF5))
	bind("messages", showPage{kind: paneMessages}, alt('m'))
	bind("output", showPage{kind: paneOutput}, alt('o'))
	bind("variables", showPage{kind: paneDebug}, alt('v'))
	bind("tree", simple{do: (*App).toggleTree}, alt('t'))
	bind("menu", simple{do: (*App).enterMenu}, plain(term.KeyF10))
	bind("help", simple{do: (*App).help}, plain(term.KeyF1))
	bind("about", simple{do: (*App).about})
}

// menus is the menu bar. The menus are shared by every IDE in the process:
// they are read and never changed.
func menus() []ui.Menu { return menuList }

// menuList is the menus, in the order the bar shows them.
var menuList = []ui.Menu{
	{Title: "~F~ile", Items: []ui.Item{
		{Label: "~N~ew", Keys: "Ctrl+N", Command: "new"},
		{Label: "New ~G~o project…", Command: "project.new"},
		{Label: "Open pro~j~ect…", Command: "project.open"},
		{Label: "Add project to go.~w~ork", Command: "work.add"},
		{},
		{Label: "~O~pen…", Keys: "F3", Command: "open"},
		{Label: "Open in ~p~roject…", Keys: "Ctrl+P", Command: "project"},
		{Label: "~S~ave", Keys: "F2", Command: "save"},
		{Label: "Save ~a~s…", Command: "save.as"},
		{Label: "Save a~l~l", Command: "save.all"},
		{Label: "~C~lose", Keys: "Alt+F3", Command: "close"},
		{},
		{Label: "E~x~it", Keys: "Alt+X", Command: "quit"},
	}},
	{Title: "~E~dit", Items: []ui.Item{
		{Label: "~U~ndo", Keys: "Ctrl+Z", Command: "undo"},
		{Label: "~R~edo", Keys: "Ctrl+Y", Command: "redo"},
		{},
		{Label: "Cu~t~", Keys: "Ctrl+X", Command: "cut"},
		{Label: "~C~opy", Keys: "Ctrl+C", Command: "copy"},
		{Label: "~P~aste", Keys: "Ctrl+V", Command: "paste"},
		{Label: "Select ~a~ll", Keys: "Ctrl+A", Command: "select.all"},
		{},
		{Label: "Clean up and ~i~mport", Keys: "Ctrl+K", Command: "cleanup"},
		{Label: "Co~m~plete", Keys: "Ctrl+Space", Command: "complete"},
	}},
	{Title: "~S~earch", Items: []ui.Item{
		{Label: "~F~ind…", Keys: "Ctrl+F", Command: "find"},
		{Label: "Find ~a~gain", Keys: "Ctrl+L", Command: "find.again"},
		{Label: "~R~eplace…", Command: "replace"},
		{Label: "~G~o to line…", Keys: "Ctrl+G", Command: "goto"},
		{},
		{Label: "Go to ~d~efinition", Keys: "F12", Command: "define"},
		{Label: "Describe ~s~ymbol", Keys: "Ctrl+D", Command: "describe"},
		{},
		{Label: "~N~ext problem", Keys: "Alt+F8", Command: "problem.next"},
		{Label: "~P~revious problem", Keys: "Alt+F7", Command: "problem.previous"},
	}},
	{Title: "~R~un", Items: []ui.Item{
		{Label: "~R~un", Keys: "Ctrl+F9", Command: "run"},
		{Label: "~A~rguments…", Command: "arguments"},
		{Label: "~T~arget…", Command: "target"},
		{},
		{Label: "~U~ser screen", Keys: "Alt+F5", Command: "user.screen"},
	}},
	{Title: "~B~uild", Items: []ui.Item{
		{Label: "~B~uild all", Keys: "F9", Command: "build"},
		{Label: "Build ~p~ackage", Keys: "Alt+F9", Command: "build.package"},
		{},
		{Label: "~T~est package", Keys: "Shift+F9", Command: "test.package"},
		{Label: "Test ~a~ll", Command: "test"},
		{Label: "~V~et", Command: "vet"},
		{},
		{Label: "Ba~z~el targets…", Keys: "Ctrl+E", Command: "targets"},
		{Label: "~R~efresh Bazel targets", Command: "targets.refresh"},
		{},
		{Label: "~S~top", Keys: "Ctrl+F2", Command: "stop"},
	}},
	{Title: "~D~ebug", Items: []ui.Item{
		{Label: "~D~ebug / continue", Keys: "F4", Command: "debug"},
		{Label: "Step ~o~ver", Keys: "F8", Command: "step.over"},
		{Label: "Step ~i~nto", Keys: "F7", Command: "step.into"},
		{Label: "Step o~u~t", Keys: "Shift+F8", Command: "step.out"},
		{Label: "~S~top", Keys: "Ctrl+F2", Command: "stop"},
		{},
		{Label: "Toggle ~b~reakpoint", Keys: "Ctrl+F8", Command: "breakpoint"},
		{Label: "~C~lear all breakpoints", Command: "breakpoints.clear"},
		{},
		{Label: "~E~valuate…", Keys: "Ctrl+F4", Command: "evaluate"},
		{Label: "Add ~w~atch…", Keys: "Ctrl+F7", Command: "watch"},
		{Label: "Clear watc~h~es", Command: "watch.clear"},
	}},
	{Title: "~W~indow", Items: []ui.Item{
		{Label: "~N~ext", Keys: "F6", Command: "window.next"},
		{Label: "~P~revious", Keys: "Shift+F6", Command: "window.previous"},
		{Label: "~L~ist…", Keys: "Alt+0", Command: "window.list"},
		{Label: "~Z~oom", Keys: "F5", Command: "zoom"},
		{},
		{Label: "Project ~t~ree", Keys: "Alt+T", Command: "tree"},
		{Label: "~M~essages", Keys: "Alt+M", Command: "messages"},
		{Label: "~O~utput", Keys: "Alt+O", Command: "output"},
		{Label: "~D~ebug", Keys: "Alt+V", Command: "variables"},
	}},
	{Title: "~H~elp", Items: []ui.Item{
		{Label: "~K~eys", Keys: "F1", Command: "help"},
		{Label: "~A~bout", Command: "about"},
	}},
}
