package ide

import (
	"errors"
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
)

func TestTheProjectTreeIsShownBesideTheTextAndHasTheKeyboardWhenNothingIsOpen(t *testing.T) {
	r := start(t)
	r.sees("═ demo ═", "▸ shapes/", "  go.mod", "  main.go", "  notes.txt", "Enter Open")
	at := r.app.layout()
	if at.tree.W != treeWidth || at.text.W != 0 {
		t.Fatalf("tree %+v, text %+v", at.tree, at.text)
	}
	r.open("main.go")
	at = r.app.layout()
	if at.text.X != treeWidth || at.text.W != 100-treeWidth || r.app.focus != focusText {
		t.Fatalf("text %+v, focus %v", at.text, r.app.focus)
	}
	r.sees("═ demo ═", " main.go ", "package main", "Alt+T Tree")
}

func TestTheTreeOpensDirectoriesAndFiles(t *testing.T) {
	r := start(t)
	r.press(term.KeyEnter).sees("▾ shapes/", "    shapes.go")
	r.press(term.KeyDown, term.KeyEnter).sees(" shapes/shapes.go ", "func Square")
	if r.app.focus != focusText {
		t.Fatal("opening a file did not give the text the keyboard")
	}
	r.alt('t').press(term.KeyLeft)
	if n, _ := r.app.chosenNode(); n.path != root+"/shapes" {
		t.Fatalf("Left on a file went to %q, not its directory", n.path)
	}
	r.press(term.KeyLeft).sees("▸ shapes/").lacks("    shapes.go")
	r.press(term.KeyRight).sees("▾ shapes/")
	r.press(term.KeyRight, term.KeyLeft, term.KeyLeft, term.KeyEnd, term.KeyRight, term.KeyEnter).sees(" notes.txt ")
	r.alt('t').write("typing in the tree goes nowhere")
	if r.text() != "plain text\n" {
		t.Fatal("typing reached the text behind the tree")
	}
	r.press(term.KeyEsc).write("x")
	if !strings.HasPrefix(r.text(), "x") {
		t.Fatal("Escape did not give the keyboard back to the text")
	}
}

func TestTheTreeIsHiddenAndShownAndAnswersTheMouse(t *testing.T) {
	r := start(t).open("main.go")
	r.alt('t').sees("Alt+T Hide")
	r.alt('t').lacks("═ demo ═")
	if at := r.app.layout(); at.tree.W != 0 || at.text.W != 100 || r.app.focus != focusText {
		t.Fatalf("a hidden tree still takes room: %+v", at)
	}
	r.alt('t').sees("═ demo ═")
	r.send(term.Event{Kind: term.EvMouse, Button: term.MouseLeft, X: 4, Y: 2}).sees("▾ shapes/")
	r.send(term.Event{Kind: term.EvMouse, Button: term.MouseLeft, X: 4, Y: 3}).sees(" shapes/shapes.go ")
	r.send(term.Event{Kind: term.EvMouse, Button: term.WheelDown, X: 4, Y: 3})
	r.send(term.Event{Kind: term.EvMouse, Button: term.WheelUp, X: 4, Y: 3})
	r.send(term.Event{Kind: term.EvMouse, Button: term.MouseLeft, X: 4, Y: 20})
}

func TestANarrowTerminalHasNoRoomForTheTreeAndTheKeyboardGoesToTheText(t *testing.T) {
	r := start(t)
	r.send(term.Event{Kind: term.EvResize, X: 60, Y: 20}).lacks("═ demo ═")
	r.ctrl('n').write("x")
	if r.text() != "x" {
		t.Fatal("the keyboard stayed with a tree that is not on the screen")
	}
	r.alt('o').press(term.KeyF5).write("y")
	if r.text() != "xy" {
		t.Fatal("the keyboard stayed with a bottom window that was zoomed away")
	}
}

func TestTheTreeShowsAFileAsSoonAsItIsSavedAndIsBounded(t *testing.T) {
	r := start(t)
	r.app.Do("new")
	r.write("package x").press(term.KeyF2)
	for range root + "/noname01.go" {
		r.press(term.KeyBackspace)
	}
	r.write("added.go").press(term.KeyEnter).sees("  added.go")
	for i := 0; i < maxTreeRows+50; i++ {
		r.files.files[root+"/many/f"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+string(rune('a'+(i/676)%26))+".go"] = ""
	}
	r.app.tree.open[root+"/many"] = true
	r.app.rebuildTree()
	if n := len(r.app.tree.nodes); n > maxTreeRows {
		t.Fatalf("%d rows", n)
	}
	r.files.failing[root+"/many"] = errors.New("unreadable")
	r.app.rebuildTree()
	r.screen()
}

func TestANewGoProjectIsMadeAndMovedInto(t *testing.T) {
	r := start(t).open("main.go")
	r.press(term.KeyF10, term.KeyEnter).write("g").sees("New Go project", "Directory to create", "/work/newproject")
	for range "newproject" {
		r.press(term.KeyBackspace)
	}
	r.write("hello").press(term.KeyEnter).sees("Module path (as in go.mod)", "hello")
	r.write("/v2").press(term.KeyEnter)
	if job := r.runner.jobs[0]; job.Dir != "/work/hello" || strings.Join(job.Args, " ") != "mod init hello/v2" {
		t.Fatalf("ran %+v", job)
	}
	r.files.files["/work/hello/go.mod"] = "module hello/v2\n\ngo 1.26\n"
	r.runner.line("go: creating new go.mod: module hello/v2")
	r.runner.done(nil)
	r.app.Drain()
	for _, name := range []string{"main.go", "README.md", ".gitignore"} {
		if _, made := r.files.files["/work/hello/"+name]; !made {
			t.Fatalf("%s was not made", name)
		}
	}
	if !strings.Contains(r.files.files["/work/hello/main.go"], "Hello from hello") || !strings.Contains(r.files.files["/work/hello/README.md"], "# hello") {
		t.Fatal("the files do not name the project")
	}
	r.sees("═ hello ═", " main.go ", "Hello from hello", "  README.md", "  go.mod").lacks("shapes/")
	if len(r.rerooted) != 1 || r.rerooted[0] != "/work/hello" {
		t.Fatalf("the language server was moved to %v", r.rerooted)
	}
	if got := r.intel.log; len(got) != 1 || got[0] != "open main.go" {
		t.Fatalf("the new project's language server was told %v", got)
	}
	r.ctrl('r')
	if len(r.runner.foreground) != 1 || r.runner.foreground[0].Dir != "/work/hello" {
		t.Fatalf("Run ran %+v", r.runner.foreground)
	}
}

func TestANewProjectIsNotMadeOverFilesAndGetsAGoModEvenWithoutTheGoTool(t *testing.T) {
	r := start(t)
	r.app.newProject(root, "demo")
	r.sees("already holds files")
	if len(r.runner.jobs) != 0 {
		t.Fatal("the Go tool was run in a directory that holds files")
	}
	r.app.newProject("/work/bare", "example.com/bare")
	r.runner.done(errors.New("exec: go: not found"))
	r.app.Drain()
	if got := r.files.files["/work/bare/go.mod"]; got != "module example.com/bare\n\ngo 1.22\n" {
		t.Fatalf("go.mod is %q", got)
	}
	r.sees("═ bare ═", " main.go ")
	r.app.newProject("/work/second", "second")
	r.app.newProject("/work/third", "third")
	r.sees("a job is already running")
	r.runner.done(nil)
	r.app.Drain()
	r.files.readOnly["/work/locked/main.go"], r.files.readOnly["/work/locked/README.md"], r.files.readOnly["/work/locked/.gitignore"] = true, true, true
	r.app.newProject("/work/locked", "locked")
	r.sees("read-only")
	r.files.readOnly["/work/nomod/go.mod"] = true
	r.app.newProject("/work/nomod", "nomod")
	r.runner.done(errors.New("no go"))
	r.app.Drain()
	r.sees("read-only")
}

func TestOpeningAnotherProjectClosesThisOneAskingAboutUnsavedEdits(t *testing.T) {
	r := start(t).open("main.go").open("notes.txt")
	old := r.intel
	r.files.files["/work/other/go.mod"] = "module other\n"
	r.files.files["/work/other/app.go"] = "package main\n"
	r.caret(0, 0).ctrl('b')
	r.write("unsaved ")
	r.app.Do("project.open")
	r.sees("Directory of the project", root)
	for range "demo" {
		r.press(term.KeyBackspace)
	}
	r.write("other").press(term.KeyEnter).sees("Save changes to notes.txt?")
	r.press(term.KeyEsc).sees("═ demo ═", " notes.txt * ")
	r.app.moveTo("/work/other", "")
	r.write("n").sees("═ other ═", "  app.go", "Project: /work/other").lacks("notes.txt")
	if len(r.app.docs) != 0 || !old.stopped || len(r.app.dbg.breaks) != 0 {
		t.Fatalf("the old project was not left behind: %d texts, server stopped %v", len(r.app.docs), old.stopped)
	}
	if got := strings.Join(old.log, ","); !strings.Contains(got, "close notes.txt") || !strings.HasSuffix(got, "close main.go") {
		t.Fatalf("the old language server was not told both texts were closed: %s", got)
	}
	r.files.failing["/work/gone"] = errors.New("no such directory")
	r.app.moveTo("/work/gone", "")
	r.sees("no such directory", "═ other ═")
	r.app.deps.Reroot = nil
	r.app.moveTo(root, root+"/main.go")
	r.sees("═ demo ═", " main.go ")
}

func TestANewProjectCompilesWhateverItsDirectoryIsCalled(t *testing.T) {
	got := projectFiles("we\"ird\\name")["main.go"]
	if !strings.Contains(got, "fmt.Println(\"Hello from we\\\"ird\\\\name\")") {
		t.Fatalf("got %s", got)
	}
}

func TestWhatTheMachineLacksIsSaidAtTheStart(t *testing.T) {
	r := start(t)
	r.app.status = "gopls not found — completion and live problems are off"
	r.sees("gopls not found")
	r.press(term.KeyDown).lacks("gopls not found")
}

func TestWhileTheLanguageServerLoadsTheStatusLineSaysSoAndThenSaysItIsReady(t *testing.T) {
	r := start(t)
	r.app.deps.Warming, r.app.warming, r.app.status = true, true, loadingNotice
	r.sees("The language server is loading the project")
	r.open("main.go")
	r.intel.hold = true
	r.caret(5, 19).press(term.KeyEnter).write("fmt.").sees("The language server is loading the project")
	r.intel.holdOffer(fmtOffers())
	r.app.Drain()
	r.sees("Language server ready", "Println")
	r.press(term.KeyEsc).ctrl(' ')
	r.intel.holdOffer(nil)
	r.app.Drain()
	r.lacks("Language server ready", "is loading")
	r.app.warming, r.app.status = true, ""
	r.intel.heard(mainPath, nil)
	r.app.Drain()
	r.sees("Language server ready")
	r.files.files["/work/other/go.mod"] = "module other\n"
	r.app.moveTo("/work/other", "")
	r.write("n")
	if !r.app.warming {
		t.Fatal("a new project's language server is taken as loaded")
	}
}

func TestTheServerBecomingReadyDoesNotTalkOverWhatWasJustSaid(t *testing.T) {
	r := start(t).open("notes.txt")
	r.app.warming = true
	r.write("x").press(term.KeyF2).sees("Saved notes.txt")
	r.intel.heard(mainPath, nil)
	r.app.Drain()
	r.sees("Saved notes.txt").lacks("Language server ready")
	if r.app.warming {
		t.Fatal("the server answered and is still taken as loading")
	}
}
