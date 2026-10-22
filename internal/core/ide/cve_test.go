package ide

import (
	"errors"
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

// Security regressions against the things the IDE listens to: a language
// server and a debugger are other programs, reading files the user did not
// write, and what they answer is not to be trusted any more than the files
// are. (The black-box half of the suite is test/cve.)

// takeover is text that would command a terminal if it reached one raw.
const takeover = "\x1b]0;pwned\x07\x1b[2J\x1b]52;c;?\x07"

// printableOnly fails when a cell of the screen holds a control character.
func printableOnly(t *testing.T, r *rig) {
	t.Helper()
	r.screen()
	for i, cell := range r.app.screen.Cells {
		if cell.R < ' ' || cell.R == 0x7f || (cell.R >= 0x80 && cell.R < 0xa0) {
			t.Fatalf("cell %d of the screen holds the control character %U", i, cell.R)
		}
	}
	for _, frame := range r.terminal.frames {
		if strings.Contains(string(frame), "\x1b]") || strings.Contains(string(frame), "\x07") {
			t.Fatalf("a hostile sequence reached the terminal: %q", frame)
		}
	}
}

func TestAHostileLanguageServerCannotCommandTheTerminal(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.offers = []Suggestion{{Label: "Print" + takeover, Detail: takeover, Insert: "Print"}}
	r.intel.about = "doc " + takeover
	r.caret(5, 19).press(term.KeyEnter).write("fmt.")
	printableOnly(t, r)
	r.press(term.KeyEsc).ctrl('d')
	printableOnly(t, r)
	r.press(term.KeyEnter)
	r.intel.heard(mainPath+takeover, []Problem{{Severe: true, Message: takeover}})
	r.app.Drain()
	r.alt('m')
	printableOnly(t, r)
}

func TestAHostileLanguageServerCannotEditOutsideTheTextOrCrashTheIDE(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.offers = []Suggestion{{Label: "Println", Insert: "Println", Extra: []Edit{
		{From: edit.Pos{Line: 999999, Col: 999999}, To: edit.Pos{Line: -5, Col: -5}, Text: "x"},
		{From: edit.Pos{Line: -1}, To: edit.Pos{Line: -1}, Text: ""},
	}}}
	r.caret(5, 19).press(term.KeyEnter).write("fmt.Pr").press(term.KeyEnter)
	if len(r.files.writes) != 0 {
		t.Fatal("a completion wrote a file")
	}
	r.intel.heard(mainPath, []Problem{{From: edit.Pos{Line: 1 << 30, Col: 1 << 30}, To: edit.Pos{Line: -9, Col: -9}, Severe: true, Message: "far away"}})
	r.app.Drain()
	r.alt('m').press(term.KeyEnter).screen()
	r.intel.place, r.intel.found = Place{Path: "/etc/shadow", At: edit.Pos{Line: 1 << 30}}, true
	r.files.failing["/etc/shadow"] = errors.New("permission denied")
	r.press(term.KeyEsc, term.KeyF12).sees("permission denied")
	if r.app.doc().path != mainPath {
		t.Fatal("a definition that cannot be read replaced the text being edited")
	}
}

func TestAHostileDebuggerOrProgramCannotCommandTheTerminal(t *testing.T) {
	r := start(t).open("main.go")
	r.caret(5, 0).ctrl('b').press(term.KeyF4)
	r.debugger.vars[100] = []debug.Variable{{Name: "s" + takeover, Value: takeover, Type: takeover, Children: 1, Ref: 3}}
	r.debugger.values[takeover] = debug.Variable{Value: takeover}
	r.debugger.watcher.Output(takeover + "\n")
	r.debugger.frames = []debug.Frame{{ID: 1, Name: "main." + takeover, File: mainPath, Line: 1 << 30, Scope: 100}}
	r.debugger.watcher.Changed(debug.Session{State: debug.Stopped})
	r.app.Drain()
	printableOnly(t, r)
	r.app.addWatch(takeover)
	r.app.evaluate(takeover)
	r.app.Drain()
	printableOnly(t, r)
	r.press(term.KeyEnter).alt('o')
	printableOnly(t, r)
	r.debugger.watcher.Changed(debug.Session{State: debug.Ended, Err: errors.New(takeover)})
	r.app.Drain()
	printableOnly(t, r)
}

func TestAnEndlessStreamOfNewsCannotGrowTheIDEWithoutBound(t *testing.T) {
	r := start(t).open("main.go")
	for i := 0; i < 3*maxOutputRows; i++ {
		r.debugger.watcher.Output("line\n")
		r.app.Drain()
	}
	if n := len(r.app.pane.rows[paneOutput]); n > maxOutputRows {
		t.Fatalf("%d rows of output are kept", n)
	}
	for i := 0; i < 40; i++ {
		r.app.addWatch(strings.Repeat("x", i+1))
	}
	if r.app.dbg.watches.Len() > debug.MaxWatches {
		t.Fatalf("%d watches are kept", r.app.dbg.watches.Len())
	}
}
