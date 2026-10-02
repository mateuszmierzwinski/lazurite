package ide

import (
	"errors"
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/core/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
)

func TestBuildSavesRunsTheGoToolAndShowsWhatItPrints(t *testing.T) {
	r := start(t).open("main.go")
	r.write("// edited\n").press(term.KeyF9)
	if len(r.files.writes) != 1 {
		t.Fatal("the changed text was not saved before the build")
	}
	if job := r.runner.jobs[0]; job.Dir != root || strings.Join(job.Args, " ") != "build ./..." {
		t.Fatalf("ran %+v", job)
	}
	r.sees("Output", "go build ./...   in .", "Build…", "Ctrl+F2 Stop")
	r.runner.done(nil)
	r.app.Drain()
	r.sees("✓ Build succeeded")
}

func TestAFailedBuildGoesToTheFirstPlaceItNamesAndTheNextKeyGoesOn(t *testing.T) {
	r := start(t).open("main.go")
	r.press(term.KeyF9)
	r.runner.line("# demo/shapes")
	r.runner.line("shapes/shapes.go:4:9: undefined: m")
	r.runner.line("./main.go:6:2: undefined: fmt.Printn")
	r.runner.done(errors.New("exit status 1"))
	r.app.Drain()
	r.sees(" shapes/shapes.go ", "╚═ 4:9 ═", "✗ Build failed: exit status 1", "Build failed")
	r.chord(term.KeyF8, term.Alt).sees(" main.go ", "╚═ 6:2 ═")
	r.chord(term.KeyF8, term.Alt).sees(" shapes/shapes.go ")
	r.chord(term.KeyF7, term.Alt).sees(" main.go ")
}

func TestPackageJobsRunInTheDirectoryOfTheTextBeingEdited(t *testing.T) {
	r := start(t).open("shapes/shapes.go")
	r.chord(term.KeyF9, term.Shift)
	if job := r.runner.jobs[0]; job.Dir != root+"/shapes" || strings.Join(job.Args, " ") != "test ." {
		t.Fatalf("ran %+v", job)
	}
	r.sees("go test .   in shapes")
	r.runner.line("--- FAIL: TestSquare (0.00s)")
	r.runner.line("    shapes_test.go:9: Square(2) = 5")
	r.runner.done(errors.New("exit status 1"))
	r.app.Drain()
	if rows := r.app.pane.rows[paneOutput]; !rows[2].jumps || rows[2].place != (Place{Path: root + "/shapes/shapes_test.go", At: edit.Pos{Line: 8}}) {
		t.Fatalf("the failing test's line does not go to it: %+v", rows[2])
	}
	r.chord(term.KeyF9, term.Alt)
	if job := r.runner.jobs[1]; strings.Join(job.Args, " ") != "build -o /dev/null ." {
		t.Fatalf("ran %+v", job)
	}
}

func TestASecondJobIsRefusedWhileOneRunsAndStopEndsIt(t *testing.T) {
	r := start(t).open("main.go")
	r.press(term.KeyF9).ctrl('t').sees("a job is already running")
	r.chord(term.KeyF2, term.Ctrl)
	if r.runner.stopped != 1 {
		t.Fatal("Stop did not stop the job")
	}
	r.runner.done(errors.New("signal: killed"))
	r.app.Drain()
	r.app.Do("test")
	r.app.Do("vet")
	if len(r.runner.jobs) != 2 || r.runner.jobs[1].Args[0] != "test" {
		t.Fatalf("ran %+v", r.runner.jobs)
	}
}

func TestAJobDoesNotStartWhenATextCannotBeSaved(t *testing.T) {
	r := start(t).open("main.go")
	r.files.failing[root+"/main.go"] = errors.New("disk full")
	r.write("x").press(term.KeyF9)
	if len(r.runner.jobs) != 0 {
		t.Fatal("a build ran on a text that is not on the disk")
	}
	r.ctrl('r').press(term.KeyF4)
	if len(r.runner.foreground) != 0 || len(r.debugger.log) != 0 {
		t.Fatal("the program was started from a text that is not on the disk")
	}
}

func TestWithNothingOpenAPackageJobRunsInTheProject(t *testing.T) {
	r := start(t)
	r.ctrl('t')
	if r.runner.jobs[0].Dir != root {
		t.Fatalf("ran in %s", r.runner.jobs[0].Dir)
	}
}

func TestOutputIsBoundedAndFollowsItsEndUnlessItIsBeingRead(t *testing.T) {
	r := start(t).open("main.go")
	r.press(term.KeyF9)
	for i := 0; i < maxOutputRows+20; i++ {
		r.runner.line("line")
		r.app.Drain()
	}
	if n := len(r.app.pane.rows[paneOutput]); n != maxOutputRows {
		t.Fatalf("%d rows are kept", n)
	}
	if r.app.pane.lists[paneOutput].Sel != maxOutputRows-1 {
		t.Fatal("the output does not follow its end")
	}
	r.alt('o').press(term.KeyHome)
	r.runner.line("more")
	r.app.Drain()
	if r.app.pane.lists[paneOutput].Sel != 0 {
		t.Fatal("output arriving moved the row being read")
	}
}

func TestRunLendsTheTerminalToTheProgram(t *testing.T) {
	r := start(t).open("main.go")
	r.app.Do("arguments")
	r.sees("Arguments the program is run with")
	r.write("-n 3  file.txt").press(term.KeyEnter)
	r.chord(term.KeyF9, term.Ctrl)
	if r.terminal.lent != 1 || len(r.runner.foreground) != 1 {
		t.Fatal("the terminal was not lent")
	}
	if job := r.runner.foreground[0]; job.Dir != root || strings.Join(job.Args, " ") != "run . -- -n 3 file.txt" {
		t.Fatalf("ran %+v", job)
	}
	r.screen()
	if last := string(r.terminal.frames[len(r.terminal.frames)-1]); !strings.Contains(last, "\x1b[2J") {
		t.Fatal("the screen was not drawn whole after the terminal came back")
	}
	r.app.Do("arguments")
	r.press(term.KeyEsc)
}

func TestRunRemembersItsProgramWhileAnotherPackageIsEdited(t *testing.T) {
	r := start(t).open("main.go")
	r.ctrl('r')
	r.open("shapes/shapes.go").ctrl('r')
	if len(r.runner.foreground) != 2 || r.runner.foreground[1].Dir != root {
		t.Fatalf("ran %+v", r.runner.foreground)
	}
}

func TestRunFindsTheProgramInTheProjectOrSaysThereIsNone(t *testing.T) {
	r := start(t).open("shapes/shapes.go")
	r.ctrl('r')
	if len(r.runner.foreground) != 1 || r.runner.foreground[0].Dir != root {
		t.Fatalf("ran %+v", r.runner.foreground)
	}
	bare := start(t)
	delete(bare.files.files, root+"/main.go")
	bare.open("shapes/shapes.go").ctrl('r').sees("no program to run")
	bare.press(term.KeyF4).sees("no program to run")
	bare.files.failing[root] = errors.New("gone")
	bare.ctrl('r').sees("no program to run")
}

func TestTheTargetCanBeSetByHand(t *testing.T) {
	r := start(t).open("shapes/shapes.go")
	r.files.files[root+"/cmd/tool/main.go"] = "package main\n\nfunc main() {}\n"
	r.app.Do("target")
	r.sees("Directory of the program")
	for range "shapes" {
		r.press(term.KeyBackspace)
	}
	r.write("cmd/tool").press(term.KeyEnter).sees("Target: cmd/tool")
	r.ctrl('r')
	if r.runner.foreground[0].Dir != root+"/cmd/tool" {
		t.Fatalf("ran in %s", r.runner.foreground[0].Dir)
	}
	r.app.Do("target")
	r.sees("cmd/tool")
}

func TestTheUserScreenWaitsWithoutRunningAnything(t *testing.T) {
	r := start(t)
	r.chord(term.KeyF5, term.Alt)
	if r.terminal.lent != 1 || len(r.runner.foreground[0].Args) != 0 {
		t.Fatalf("ran %+v", r.runner.foreground)
	}
	r.runner.failing = errors.New("no terminal")
	r.chord(term.KeyF5, term.Alt).sees("no terminal")
}
