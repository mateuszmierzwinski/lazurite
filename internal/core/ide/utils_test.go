package ide

import (
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/termkit/term"
)

func TestDisplayNameIsRelativeInsideTheProjectAndWholeOutside(t *testing.T) {
	for path, want := range map[string]string{
		"/work/demo/main.go":       "main.go",
		"/work/demo/a/b.go":        "a/b.go",
		"/work/demo":               ".",
		"/work/demolition/x.go":    "/work/demolition/x.go",
		"/usr/lib/go/src/fmt/p.go": "/usr/lib/go/src/fmt/p.go",
	} {
		if got := displayName("/work/demo", path); got != want {
			t.Errorf("displayName(%q) = %q, want %q", path, got, want)
		}
	}
	if displayName("", "/x/y.go") != "/x/y.go" {
		t.Fatal("with no project a path is shown whole")
	}
}

func TestParsePlaceReadsCompilerAndTestLines(t *testing.T) {
	for line, want := range map[string]Place{
		"./main.go:12:5: undefined: x":           {Path: "./main.go", At: edit.Pos{Line: 12, Col: 5}},
		"    shapes_test.go:15: got 5":           {Path: "shapes_test.go", At: edit.Pos{Line: 15}},
		"/abs/path/a.go:3:1: syntax error":       {Path: "/abs/path/a.go", At: edit.Pos{Line: 3, Col: 1}},
		"vet: internal/x/y.go:7:2: unusedresult": {},
	} {
		file, ln, col, ok := parsePlace(line)
		if ok != (want.Path != "") || file != want.Path || ln != want.At.Line || col != want.At.Col {
			t.Errorf("parsePlace(%q) = %q %d %d %v", line, file, ln, col, ok)
		}
	}
	for _, line := range []string{"", "ok  demo 0.01s", "# demo/shapes", "main.go:0:1: x", "main.go:99999999999999999999: x", "FAIL"} {
		if _, _, _, ok := parsePlace(line); ok {
			t.Errorf("parsePlace(%q) found a place", line)
		}
	}
}

func TestMainLineFindsWhereAProgramBegins(t *testing.T) {
	if line, ok := mainLine(mainSource); !ok || line != 4 {
		t.Fatalf("got %d %v", line, ok)
	}
	for _, text := range []string{"package shapes\n\nfunc main() {}\n", "package main\n\nfunc mainly() {}\n", "package main\n// func main()\n", ""} {
		if _, ok := mainLine(text); ok {
			t.Errorf("mainLine(%q) found one", text)
		}
	}
	if !declaresMain("// c\npackage main // x\n") || declaresMain("package mainframe\n") || declaresMain(" package main\n") {
		t.Fatal("declaresMain")
	}
}

func TestRelocateKeepsTheCaretOnItsLine(t *testing.T) {
	old := strings.Split("package x\nimport \"fmt\"\n\nfunc f() {\n\tfmt.Println()\n}\n", "\n")
	fresh := strings.Split("package x\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc f() {\n\tfmt.Println()\n}\n", "\n")
	for line, want := range map[int]int{0: 0, 4: 8, 5: 9, 6: 10, 1: 5} {
		if got := relocate(old, fresh, line); got != want {
			t.Errorf("relocate(%d) = %d, want %d", line, got, want)
		}
	}
	if relocate([]string{"a", "b"}, []string{"x"}, 1) != 0 || relocate([]string{"a"}, nil, 0) != 0 {
		t.Fatal("a line that is gone is not brought inside the text")
	}
}

func TestOneLineIsFitToShow(t *testing.T) {
	if got := oneLine("  first\x1b[31m line\nsecond"); got != "first [31m line" {
		t.Fatalf("got %q", got)
	}
	long := oneLine(strings.Repeat("x", 500))
	if len([]rune(long)) != maxStatus+1 || !strings.HasSuffix(long, "…") {
		t.Fatalf("%d runes", len([]rune(long)))
	}
	if oneLine("") != "" {
		t.Fatal("empty")
	}
}

func TestExpandTabsOpensThemToTheTabStops(t *testing.T) {
	if got := expandTabs("a\tb\t\tc"); got != "a   b       c" {
		t.Fatalf("got %q", got)
	}
	if expandTabs("plain") != "plain" {
		t.Fatal("a line with no tab changed")
	}
}

func TestWrapBreaksAtSpacesKeepsLineBreaksAndIsBounded(t *testing.T) {
	got := wrap("one two three four five\n\nsix\r\nseven_is_a_very_long_word_indeed", 10)
	want := []string{"one two", "three four", "five", "", "six", "seven_is_a", "_very_long", "_word_inde", "ed"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	if n := len(wrap(strings.Repeat("x\n", 5000), 10)); n != maxWrapped+1 {
		t.Fatalf("%d lines", n)
	}
	if len(wrap("abc", 0)) != 3 {
		t.Fatal("a width of nothing is taken as one")
	}
}

func TestLineAndColumnReadsWhatAPersonTypes(t *testing.T) {
	for typed, want := range map[string]edit.Pos{"12": {Line: 11}, " 3 : 4 ": {Line: 2, Col: 3}, "5:x": {Line: 4}, "7:0": {Line: 6}} {
		if got, ok := lineAndColumn(typed); !ok || got != want {
			t.Errorf("lineAndColumn(%q) = %+v %v", typed, got, ok)
		}
	}
	for _, typed := range []string{"", "x", "0", "-3", ":4"} {
		if _, ok := lineAndColumn(typed); ok {
			t.Errorf("lineAndColumn(%q) was taken", typed)
		}
	}
}

func TestProblemsAreListedMarkedAndWalked(t *testing.T) {
	r := start(t).open("main.go")
	r.intel.heard(root+"/shapes/shapes.go", []Problem{{From: edit.Pos{Line: 3, Col: 8}, To: edit.Pos{Line: 3, Col: 9}, Severe: true, Message: "undefined: m"}})
	r.intel.heard(mainPath, []Problem{{From: edit.Pos{Line: 5, Col: 1}, To: edit.Pos{Line: 5, Col: 4}, Message: "unused\x1b[2J result\nmore"}})
	r.app.Drain()
	r.alt('m').sees("Messages", "main.go:6:2: warning: unused [2J result", "shapes/shapes.go:4:9: error: undefined: m")
	if !r.app.screen.At(treeWidth+5, 7).S.Underline {
		t.Fatal("the problem is not marked in the text")
	}
	r.press(term.KeyDown, term.KeyEnter).sees(" shapes/shapes.go ", "╚═ 4:9 ═")
	r.chord(term.KeyF8, term.Alt).sees(" main.go ", "╚═ 6:2 ═")
	r.intel.heard(mainPath, nil)
	r.intel.heard(root+"/shapes/shapes.go", nil)
	r.app.Drain()
	r.chord(term.KeyF8, term.Alt).sees("No problems")
}

func TestTheProblemListIsBounded(t *testing.T) {
	r := start(t)
	many := make([]Problem, maxProblemRows+100)
	r.intel.heard(mainPath, many)
	r.app.Drain()
	if n := len(r.app.pane.rows[paneMessages]); n != maxProblemRows {
		t.Fatalf("%d rows", n)
	}
}

func TestTheBottomWindowIsShownZoomedAwayAndTakesTheMouse(t *testing.T) {
	r := start(t).open("main.go")
	r.alt('o').sees("Output", "Alt+M Messages · Alt+O Output · Alt+V Debug", "Enter Go to")
	r.write("typing goes nowhere while the window has the keyboard")
	if r.text() != mainSource {
		t.Fatal("typing reached the text behind the bottom window")
	}
	r.press(term.KeyEsc).write("x")
	if !strings.HasPrefix(r.text(), "x") {
		t.Fatal("Escape did not give the keyboard back")
	}
	r.press(term.KeyF5).lacks("Alt+M Messages")
	r.press(term.KeyF5)
	r.app.print("first")
	r.app.print("second")
	bottom := r.app.layout().bottom
	r.send(term.Event{Kind: term.EvMouse, Button: term.MouseLeft, X: 4, Y: bottom.Y + 1})
	if r.app.focus != focusPane || r.app.pane.lists[paneOutput].Sel != 0 {
		t.Fatal("a press did not choose the row")
	}
	r.send(term.Event{Kind: term.EvMouse, Button: term.WheelDown, X: 4, Y: bottom.Y + 1})
	r.send(term.Event{Kind: term.EvMouse, Button: term.WheelUp, X: 4, Y: bottom.Y + 1})
	r.press(term.KeyEnter, term.KeyF9)
}

func TestWithNoTextOpenTheBottomWindowStillShows(t *testing.T) {
	r := start(t)
	r.alt('m').sees("Messages", "░")
	r.press(term.KeyEnter)
}

func TestRunStopsWhenTheTerminalGoesAwayOrTheUserLeaves(t *testing.T) {
	r := start(t).open("main.go")
	r.terminal.events <- term.Event{Kind: term.EvKey, Key: term.KeyRight}
	r.terminal.events <- term.Event{Kind: term.EvKey, Key: term.KeyRune, Rune: 'x', Mods: term.Alt}
	if err := r.app.Run(); err != nil || !r.intel.stopped {
		t.Fatalf("Run = %v, language server stopped: %v", err, r.intel.stopped)
	}
	gone := start(t)
	close(gone.terminal.events)
	if err := gone.app.Run(); err != nil {
		t.Fatalf("Run = %v", err)
	}
	gone.app.post(func() { t.Fatal("work posted after the loop ended was run") })
}

func TestRunDoesPostedWorkAndReportsATerminalThatCannotBeWritten(t *testing.T) {
	r := start(t)
	r.app.deps.Spawn = nil
	ran := make(chan struct{})
	r.app.spawn(func() {
		r.app.post(func() { close(ran) })
		<-ran
		r.app.post(func() { r.app.quit = true })
	})
	if err := r.app.Run(); err != nil {
		t.Fatalf("Run = %v", err)
	}
	broken := start(t)
	broken.terminal.failOn = errTerminalGone
	if err := broken.app.Run(); err != errTerminalGone {
		t.Fatalf("Run = %v", err)
	}
}

func TestNoIntelAnswersNothing(t *testing.T) {
	n := NoIntel()
	n.Listen(nil)
	n.Open("", "")
	n.Change("", "")
	n.Save("", "")
	n.Close("")
	n.Stop()
	n.Complete("", edit.Pos{}, func(s []Suggestion) {
		if s != nil {
			t.Fatal("completions from nothing")
		}
	})
	n.Define("", edit.Pos{}, func(_ Place, ok bool) {
		if ok {
			t.Fatal("a definition from nothing")
		}
	})
	n.Describe("", edit.Pos{}, func(text string) {
		if text != "" {
			t.Fatal("a description from nothing")
		}
	})
}

func TestValueAndWatchText(t *testing.T) {
	if valueText(debug.Variable{Value: "3"}) != "3" || valueText(debug.Variable{Value: "a\nb", Type: "string"}) != "a  (string)" {
		t.Fatal("valueText")
	}
	if frameRow(debug.Frame{Name: "runtime.main"}).jumps {
		t.Fatal("a frame with no source goes somewhere")
	}
}

func BenchmarkDisplayName(b *testing.B) {
	for b.Loop() {
		displayName("/work/demo", "/work/demo/internal/core/x.go")
	}
}

func BenchmarkParsePlace(b *testing.B) {
	for b.Loop() {
		parsePlace("./internal/core/ide/app.go:120:14: undefined: x")
	}
}

func BenchmarkMainLine(b *testing.B) {
	for b.Loop() {
		mainLine(mainSource)
	}
}

func BenchmarkDeclaresMain(b *testing.B) {
	for b.Loop() {
		declaresMain(mainSource)
	}
}

func BenchmarkRelocate(b *testing.B) {
	old := strings.Split(strings.Repeat("line\n", 500), "\n")
	fresh := append([]string{"new"}, old...)
	for b.Loop() {
		relocate(old, fresh, 250)
	}
}

func BenchmarkCommonPrefix(b *testing.B) {
	lines := strings.Split(strings.Repeat("line\n", 500), "\n")
	for b.Loop() {
		commonPrefix(lines, lines)
	}
}

func BenchmarkCommonSuffix(b *testing.B) {
	lines := strings.Split(strings.Repeat("line\n", 500), "\n")
	for b.Loop() {
		commonSuffix(lines, lines)
	}
}

func BenchmarkOneLine(b *testing.B) {
	for b.Loop() {
		oneLine("main.go:3:1: expected declaration, found x\nand more")
	}
}

func BenchmarkExpandTabs(b *testing.B) {
	for b.Loop() {
		expandTabs("\tif err != nil {\treturn err\t}")
	}
}

func BenchmarkWrap(b *testing.B) {
	text := strings.Repeat("Println formats using the default formats for its operands. ", 8)
	for b.Loop() {
		wrap(text, 64)
	}
}

func BenchmarkWrapLine(b *testing.B) {
	text := strings.Repeat("Println formats using the default formats for its operands. ", 8)
	for b.Loop() {
		wrapLine(text, 64)
	}
}

func BenchmarkLineAndColumn(b *testing.B) {
	for b.Loop() {
		lineAndColumn("120:14")
	}
}

func BenchmarkPaint(b *testing.B) {
	r := start(&testing.T{})
	_ = r.app.Open(mainPath)
	for b.Loop() {
		_ = r.app.Paint()
	}
}
