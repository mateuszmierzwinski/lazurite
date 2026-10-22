package ide

import (
	"errors"
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/ui"
)

const mainPath = root + "/main.go"

func TestABreakpointIsToggledOnTheCaretsLineAndDrawn(t *testing.T) {
	r := start(t).open("main.go")
	r.caret(5, 0).chord(term.KeyF8, term.Ctrl).screen()
	if r.app.screen.At(treeWidth+1, 7).S != ui.BreakLine {
		t.Fatal("the line with the breakpoint is not drawn as one")
	}
	r.ctrl('b').screen()
	if r.app.screen.At(treeWidth+1, 7).S == ui.BreakLine || len(r.app.dbg.breaks[mainPath]) != 0 {
		t.Fatal("the breakpoint was not removed")
	}
	r.ctrl('n').ctrl('b').sees("save the file first")
}

func TestDebuggingLaunchesHeldSetsTheBreakpointsThenLetsTheProgramGo(t *testing.T) {
	r := start(t).open("main.go")
	r.caret(5, 0).ctrl('b').press(term.KeyF4)
	if got := strings.Join(r.debugger.log, ","); got != "launch,breakpoints main.go,continue" {
		t.Fatalf("the debugger was asked: %s", got)
	}
	if got := strings.Join(r.debugger.launched, " "); got != "go "+root+" run ." {
		t.Fatalf("launched %q", got)
	}
	if p := r.debugger.points[mainPath]; len(p) != 1 || p[0].Line != 6 {
		t.Fatalf("breakpoints %+v — the debugger counts lines from one", p)
	}
	r.sees("Debugging .", "Ctrl+F2 Stop")
	r.press(term.KeyF4).sees("The program is running")
	r.press(term.KeyF8).sees("The program is running")
}

func TestAStopShowsTheLineTheStackAndTheVariables(t *testing.T) {
	r := start(t).open("shapes/shapes.go").open("main.go")
	r.caret(5, 0).ctrl('b').press(term.KeyF4)
	r.debugger.vars[100] = []debug.Variable{{Name: "total", Value: "3", Type: "int"}, {Name: "items", Value: "[]int len: 2", Type: "[]int", Children: 2, Ref: 7}}
	r.debugger.vars[7] = []debug.Variable{{Name: "[0]", Value: "1", Type: "int"}, {Name: "[1]", Value: "2", Type: "int"}}
	r.open("shapes/shapes.go")
	r.debugger.stops(mainPath, 6)
	r.app.Drain()
	r.sees(" main.go ", "╚═ 6:1 ═", "Debug", "Call stack", "main.main   main.go:6", "runtime.main", "Variables", "total = 3  (int)", "+ items = []int len: 2", "F8 Over", "F4 Continue")
	if r.app.screen.At(treeWidth+1, 7).S != ui.ResumeLine {
		t.Fatal("the line the program rests on is not drawn as it")
	}
	r.alt('v').press(term.KeyDown, term.KeyDown, term.KeyEnter)
	if got := r.page(paneDebug); !strings.Contains(got, "- items = []int len: 2  ([]int)\n      [0] = 1  (int)\n      [1] = 2  (int)\n") {
		t.Fatalf("the variable was not opened:\n%s", got)
	}
	r.press(term.KeyEnter)
	if got := r.page(paneDebug); strings.Contains(got, "[0] = 1") || !strings.Contains(got, "+ items") {
		t.Fatalf("the variable was not closed:\n%s", got)
	}
	r.press(term.KeyUp, term.KeyEnter, term.KeyHome, term.KeyEnter)
	r.caret(0, 0).alt('v').press(term.KeyEnd, term.KeyUp, term.KeyEnter).sees("╚═ 6:1 ═")
}

func TestSteppingAsksTheDebuggerAndContinueLetsItGo(t *testing.T) {
	r := start(t).open("main.go")
	r.caret(5, 0).ctrl('b').press(term.KeyF4)
	r.debugger.stops(mainPath, 6)
	r.app.Drain()
	r.press(term.KeyF8)
	r.debugger.stops(mainPath, 7)
	r.app.Drain()
	r.press(term.KeyF7)
	r.debugger.stops(mainPath, 7)
	r.app.Drain()
	r.chord(term.KeyF8, term.Shift)
	r.debugger.stops(mainPath, 7)
	r.app.Drain()
	r.press(term.KeyF4)
	if got := strings.Join(r.debugger.log[3:], ","); got != "step,stepin,stepout,continue" {
		t.Fatalf("the debugger was asked: %s", got)
	}
	r.lacks("F8 Over")
}

func TestSteppingWithNothingStartedStopsAtTheFirstLineOfMain(t *testing.T) {
	r := start(t).open("shapes/shapes.go").open("main.go")
	r.press(term.KeyF8)
	if p := r.debugger.points[mainPath]; len(p) != 1 || p[0].Line != 5 {
		t.Fatalf("breakpoints %+v, want one on `func main`", p)
	}
	r.debugger.stops(mainPath, 5)
	r.app.Drain()
	if p := r.debugger.points[mainPath]; len(p) != 0 {
		t.Fatalf("the breakpoint that stopped at main was left behind: %+v", p)
	}
	r.screen()
	if r.app.screen.At(treeWidth+1, 6).S != ui.ResumeLine {
		t.Fatal("the program does not rest on main")
	}
}

func TestABreakpointSetWhileTheProgramRunsIsSentAtTheNextStop(t *testing.T) {
	r := start(t).open("main.go")
	r.caret(5, 0).ctrl('b').press(term.KeyF4)
	sent := len(r.debugger.log)
	r.caret(4, 0).ctrl('b')
	if len(r.debugger.log) != sent {
		t.Fatal("the debugger was asked while the program ran: it would wait for the program")
	}
	r.debugger.stops(mainPath, 6)
	r.app.Drain()
	if p := r.debugger.points[mainPath]; len(p) != 2 || p[0].Line != 5 || p[1].Line != 6 {
		t.Fatalf("breakpoints %+v", p)
	}
	r.app.Do("breakpoints.clear")
	r.app.Drain()
	if p := r.debugger.points[mainPath]; len(p) != 0 {
		t.Fatalf("breakpoints %+v after clearing", p)
	}
	r.sees("Breakpoints cleared")
}

func TestABreakpointTheDebuggerRefusesIsReported(t *testing.T) {
	r := start(t).open("main.go")
	r.debugger.refuse = true
	r.caret(1, 0).ctrl('b').press(term.KeyF4).alt('o').sees("Breakpoint not set at main.go:2: no code here")
}

func TestTheProgramEndingClearsTheRestAndSaysSo(t *testing.T) {
	r := start(t).open("main.go")
	r.caret(5, 0).ctrl('b').press(term.KeyF4)
	r.debugger.watcher.Output("total: 6\n")
	r.debugger.stops(mainPath, 6)
	r.app.Drain()
	r.debugger.watcher.Changed(debug.Session{State: debug.Ended})
	r.app.Drain()
	r.alt('o').sees("total: 6", "Program ended").lacks("F8 Over")
	if r.app.dbg.at || r.app.resumeLine(mainPath) != -1 {
		t.Fatal("the program is gone and still rests somewhere")
	}
	if stops := strings.Count(strings.Join(r.debugger.log, ","), "stop"); stops != 1 {
		t.Fatalf("the debugger was told to stop %d times", stops)
	}
	r.alt('v').sees("Nothing is being debugged")
	r.press(term.KeyF4)
	if strings.Count(strings.Join(r.debugger.log, ","), "launch") != 2 {
		t.Fatal("a second session could not be started")
	}
}

func TestStopEndsTheSessionAndAFailedLaunchIsReported(t *testing.T) {
	r := start(t).open("main.go")
	r.press(term.KeyF4).chord(term.KeyF2, term.Ctrl)
	if r.debugger.log[len(r.debugger.log)-1] != "stop" {
		t.Fatalf("the debugger was asked: %v", r.debugger.log)
	}
	r.sees("Program ended")
	r.chord(term.KeyF2, term.Ctrl)
	r.debugger.failing = errors.New("build failed: main.go:3:1: syntax error")
	r.press(term.KeyF4).sees("build failed: main.go:3:1: syntax error")
	r.alt('o').sees("✗ build failed")
}

func TestEvaluateAndWatchesAreWorkedOutInTheRestingProgram(t *testing.T) {
	r := start(t).open("main.go")
	r.chord(term.KeyF4, term.Ctrl).write("total").press(term.KeyEnter).sees("The program is not stopped")
	r.caret(5, 0).ctrl('b').press(term.KeyF4)
	r.debugger.values["total*2"] = debug.Variable{Value: "12", Type: "int"}
	r.debugger.stops(mainPath, 6)
	r.app.Drain()
	r.caret(5, 3).chord(term.KeyF4, term.Ctrl).sees("Expression", "fmt")
	for range "fmt" {
		r.press(term.KeyBackspace)
	}
	r.write("total*2").press(term.KeyEnter).sees("Evaluate", "total*2", "= 12  (int)")
	r.press(term.KeyEnter)
	r.chord(term.KeyF4, term.Ctrl).write("x").press(term.KeyEnter).sees("Evaluate")
	r.press(term.KeyEnter)
	r.chord(term.KeyF7, term.Ctrl)
	for range "fmt" {
		r.press(term.KeyBackspace)
	}
	r.write("total*2").press(term.KeyEnter).sees("Watches", "total*2 = 12  (int)")
	r.app.addWatch("total*2")
	r.sees("Not added")
	r.app.addWatch("nope")
	r.app.Drain()
	r.sees("nope : ")
	r.press(term.KeyF8).alt('v').sees("total*2 = …")
	r.app.Do("watch.clear")
	r.lacks("Watches")
}

func TestTheWordAtTheCaretStartsAnExpression(t *testing.T) {
	r := start(t).open("main.go")
	r.caret(5, 7)
	if got := r.app.wordAtCaret(); got != "Println" {
		t.Fatalf("got %q", got)
	}
	r.app.editor().Select(edit.Pos{Line: 5, Col: 1}, edit.Pos{Line: 5, Col: 12})
	if got := r.app.wordAtCaret(); got != "fmt.Println" {
		t.Fatalf("got %q", got)
	}
	if start(t).app.wordAtCaret() != "" {
		t.Fatal("a word with nothing open")
	}
}

func TestABuildWithNoDebuggerSaysSo(t *testing.T) {
	r := start(t)
	r.app.debugger = nil
	r.press(term.KeyF4).sees("This build has no debugger")
	r.press(term.KeyF8).sees("This build has no debugger")
	r.chord(term.KeyF2, term.Ctrl)
}

func TestAStopWithNoSourceAndAnAnswerAfterTheProgramLeftAreHarmless(t *testing.T) {
	r := start(t).open("main.go")
	r.press(term.KeyF4)
	r.debugger.frames = []debug.Frame{{ID: 1, Name: "runtime.goexit"}}
	r.debugger.watcher.Changed(debug.Session{State: debug.Stopped})
	r.app.Drain()
	r.sees("runtime.goexit")
	if r.app.dbg.at {
		t.Fatal("a frame with no source is somewhere to rest")
	}
	r.app.showFrames(nil, errors.New("gone"))
	r.app.showVariables(nil, errors.New("gone"))
	r.app.openVariable("x", 1, nil, errors.New("gone"))
	r.app.toggleVariable(99)
	r.debugger.watcher.Changed(debug.Session{State: debug.Ended})
	r.app.Drain()
	r.app.showFrames([]debug.Frame{{File: mainPath, Line: 3}}, nil)
	if r.app.dbg.at {
		t.Fatal("frames of a program that is gone were shown")
	}
	r.app.fetchVariables()
}

func TestVariablesAreBounded(t *testing.T) {
	vars := []variable{{v: debug.Variable{Name: "big", Children: 10000}}}
	many := make([]debug.Variable, 10000)
	if got := insertChildren(vars, 0, many); len(got) != maxVariables || !got[0].open {
		t.Fatalf("%d lines", len(got))
	}
}
