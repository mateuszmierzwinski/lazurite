// Package e2e_test drives the real IDE — the wiring the binary uses, the
// real Go tool, language server and debugger — over a real project, the
// way a person would: by keys, reading the screen.
package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
	"github.com/mateuszmierzwinski/lazurite/test/stack"
)

// patience is how long the compiler, the language server or the debugger
// is waited for.
const patience = 120 * time.Second

// project is a small program in two packages and two directories.
func project() map[string]string {
	return map[string]string{
		"go.mod": "module demo\n\ngo 1.24\n",
		"main.go": `package main

import (
	"fmt"

	"demo/shapes"
)

func main() {
	total := 0
	for i := 1; i <= 3; i++ {
		total += shapes.Square(i)
	}
	fmt.Println("total:", total)
}
`,
		"shapes/shapes.go":      "// Package shapes does sums.\npackage shapes\n\n// Square is n times n.\nfunc Square(n int) int {\n\treturn n * n\n}\n",
		"shapes/shapes_test.go": "package shapes\n\nimport \"testing\"\n\nfunc TestSquare(t *testing.T) {\n\tif Square(3) != 9 {\n\t\tt.Fatal(\"Square(3)\")\n\t}\n}\n",
	}
}

func TestSavingCleansTheCodeUpAndAddsTheImportsItUses(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{}).Open("main.go")
	s.GoTo("13").Press(term.KeyEnd, term.KeyEnter)
	s.Type(`fmt.Println(  strings.Repeat("ab",2),os.Args )`).Press(term.KeyF2)
	got := s.File("main.go")
	if !strings.Contains(got, "\t\"os\"\n\t\"strings\"\n") {
		t.Fatalf("the imports were not added:\n%s", got)
	}
	if !strings.Contains(got, "\tfmt.Println(strings.Repeat(\"ab\", 2), os.Args)\n") {
		t.Fatalf("the line was not formatted:\n%s", got)
	}
	s.Sees("Saved main.go", "\"strings\"", "╚═ 16:").Lacks(" main.go * ")
	s.GoTo("3").Press(term.KeyEnter).Ctrl('z').Ctrl('k').Sees("Cleaned up")
}

func TestAProjectOfSeveralPackagesIsBuiltAndAFaultInAnotherDirectoryIsJumpedTo(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{}).Open("main.go")
	s.Press(term.KeyF9).Awaits("Build succeeded", patience)
	s.Ctrl('p').Sees("Open in project", "shapes/shapes.go", "shapes/shapes_test.go", "go.mod")
	s.Type("shapes.go").Press(term.KeyEnter).Sees(" shapes/shapes.go ")
	s.GoTo("6").Press(term.KeyEnd).Type(" + missing").Alt('1').Sees(" main.go ")
	s.Press(term.KeyF9).Awaits("Build failed", patience)
	s.Sees(" shapes/shapes.go ", "undefined: missing", "╚═ 6:15 ═")
	if !strings.Contains(s.File("shapes/shapes.go"), "+ missing") {
		t.Fatal("the build ran on a text that was not saved first")
	}
}

func TestAPackagesTestsAreRunAndAFailureIsAPlaceToGo(t *testing.T) {
	stack.NeedsGo(t)
	files := project()
	files["shapes/shapes_test.go"] = "package shapes\n\nimport \"testing\"\n\nfunc TestSquare(t *testing.T) {\n\tif Square(3) != 10 {\n\t\tt.Fatal(\"Square(3) is not ten\")\n\t}\n}\n"
	s := stack.Start(t, files, stack.Options{}).Open("shapes/shapes.go")
	s.Chord(term.KeyF9, term.Shift).Awaits("Test package failed", patience)
	s.Sees("go test .   in shapes", "Square(3) is not ten", " shapes/shapes_test.go ", "╚═ 7:1 ═")
	stack.Put(t, s.Path("shapes/shapes_test.go"), project()["shapes/shapes_test.go"])
	s.Alt('1').Ctrl('t').Awaits("Test package succeeded", patience)
}

func TestRunningHandsTheTerminalToTheProgramAndTakesItBack(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{}).Open("shapes/shapes.go")
	s.Chord(term.KeyF9, term.Ctrl)
	out := s.Out.String()
	if !strings.Contains(out, "total: 14") || !strings.Contains(out, "press Enter to return to Lazurite") {
		t.Fatalf("the program's own screen shows:\n%s", out)
	}
	s.Sees(" shapes/shapes.go ", "F10 Menu")
}

func TestAProgramIsDebuggedStoppedSteppedInspectedAndLetGo(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{}).Open("main.go")
	s.GoTo("12").Chord(term.KeyF8, term.Ctrl).Press(term.KeyF4)
	if !s.Shows("Call stack", patience) {
		t.Skipf("this machine will not let a program be debugged:\n%s", s.Screen())
	}
	s.Awaits("i = 1  (int)", patience).Sees("main.main   main.go:12", "total = 0  (int)", "╚═ 12:1 ═", "F8 Over")
	s.Press(term.KeyF7).Awaits("shapes.Square   shapes.go:5", patience).Awaits("n = 1  (int)", patience).Sees(" shapes/shapes.go ")
	s.Chord(term.KeyF8, term.Shift).Awaits("main.main   main.go:12", patience)
	s.Press(term.KeyF4).Awaits("i = 2  (int)", patience)
	s.Chord(term.KeyF4, term.Ctrl).Type("total * 10").Press(term.KeyEnter).Awaits("= 10  (int)", patience)
	s.Press(term.KeyEnter)
	s.Chord(term.KeyF7, term.Ctrl).Type("i + 100").Press(term.KeyEnter).Awaits("i + 100 = 102", patience)
	s.GoTo("12").Chord(term.KeyF8, term.Ctrl).Press(term.KeyF4).Awaits("Program ended", patience)
	s.Alt('o').Sees("total: 14").Lacks("F8 Over")
}

func TestSteppingFromNothingStopsAtTheStartOfMainAndStopEndsTheProgram(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{}).Open("shapes/shapes.go")
	s.Press(term.KeyF8)
	if !s.Shows("Call stack", patience) {
		t.Skipf("this machine will not let a program be debugged:\n%s", s.Screen())
	}
	s.Sees(" main.go ", "main.main   main.go:9")
	s.Chord(term.KeyF2, term.Ctrl).Awaits("Program ended", patience)
	s.Press(term.KeyF8).Awaits("Call stack", patience)
}

func TestTheLanguageServerCompletesReportsAndFindsDefinitions(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{Gopls: stack.NeedsGopls(t)}).Open("main.go")
	s.GoTo("13").Press(term.KeyEnd, term.KeyEnter).Type("fmt.Pr")
	s.Awaits("Println  func(a ...any)", patience)
	s.Press(term.KeyDown, term.KeyDown, term.KeyEnter).Sees("    fmt.Println")
	s.Type("(notDeclared)").Alt('m').Awaits("undefined: notDeclared", patience)
	s.Press(term.KeyEnter).Sees("╚═ 14:14 ═")
	s.GoTo("12:20").Press(term.KeyF12).Awaits(" shapes/shapes.go ", patience)
	s.Sees("╚═ 5:6 ═")
	s.Ctrl('d').Awaits("func Square(n int) int", patience)
}

func TestANewGoProjectIsCreatedOnDiskAndIsReadyToRun(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{}).Open("main.go")
	s.App.Do("project.new")
	s.Sees("New Go project", "Directory to create")
	for range "newproject" {
		s.Press(term.KeyBackspace)
	}
	s.Type("greeter").Press(term.KeyEnter).Sees("Module path")
	for range "greeter" {
		s.Press(term.KeyBackspace)
	}
	s.Type("example.com/greeter").Press(term.KeyEnter)
	s.Awaits("═ greeter ═", patience).Sees(" main.go ", "Hello from greeter", "  README.md", "  go.mod")
	dir := filepath.Join(filepath.Dir(s.Dir), "greeter")
	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil || !strings.HasPrefix(string(mod), "module example.com/greeter\n") || !strings.Contains(string(mod), "\ngo ") {
		t.Fatalf("go.mod: %q, %v", mod, err)
	}
	for _, name := range []string{"main.go", "README.md", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s was not made: %v", name, err)
		}
	}
	s.Press(term.KeyF9).Awaits("Build succeeded", patience)
	s.Chord(term.KeyF9, term.Ctrl)
	if out := s.Out.String(); !strings.Contains(out, "Hello from greeter") {
		t.Fatalf("the new project's program printed:\n%s", out)
	}
}

func TestTheProjectTreeWalksPackagesAndOpensTheirFiles(t *testing.T) {
	s := stack.Start(t, project(), stack.Options{})
	s.Sees("▸ shapes/", "  go.mod", "  main.go", "Enter Open")
	s.Press(term.KeyEnter).Sees("▾ shapes/", "    shapes.go", "    shapes_test.go")
	s.Press(term.KeyDown, term.KeyEnter).Sees(" shapes/shapes.go ", "func Square(n int) int")
	s.Alt('t').Press(term.KeyEnd, term.KeyEnter).Sees(" main.go ", "total += shapes.Square(i)")
}

func TestANewFileIsCompletedBeforeItIsEverSaved(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{Gopls: stack.NeedsGopls(t)})
	s.Ctrl('n').Type("package main").Press(term.KeyEnter, term.KeyEnter).Type("import \"fmt\"").Press(term.KeyEnter, term.KeyEnter)
	s.Type("func extra() {").Press(term.KeyEnter).Type("fmt.Pr")
	s.Awaits("Println  func(a ...any)", patience)
}

// TestAModuleItsGoWorkLeavesOutIsStillBuiltAndCompleted — the whole Go
// toolchain refuses such a module ("directory prefix . does not contain
// modules listed in go.work"), and with it the language server: the editor
// just looks broken. The IDE sees it and runs its tools standalone.
func TestAModuleItsGoWorkLeavesOutIsStillBuiltAndCompleted(t *testing.T) {
	stack.NeedsGo(t)
	// The machine running this test may have GOWORK set — which the IDE
	// rightly takes as the user's own answer and leaves alone. The test is
	// about a user who has not set it.
	t.Setenv("GOWORK", "")
	files := map[string]string{"go.work": "go 1.24\n\nuse ./listed\n", "listed/go.mod": "module listed\n\ngo 1.24\n", "listed/l.go": "package listed\n"}
	for name, text := range project() {
		files["app/"+name] = text
	}
	s := stack.Start(t, files, stack.Options{Sub: "app", Gopls: stack.NeedsGopls(t)}).Open("main.go")
	s.Sees("╚═ go · GOWORK=off ═")
	s.Press(term.KeyF9).Awaits("Build succeeded", patience)
	s.GoTo("13").Press(term.KeyEnd, term.KeyEnter).Type("fmt.Pr").Awaits("Println  func(a ...any)", patience)
	s.Press(term.KeyDown, term.KeyDown, term.KeyEnter).Type(`("completed")`)
	s.App.Do("work.add")
	s.Sees("Added to", "╚═ go ═")
	work, err := os.ReadFile(filepath.Join(filepath.Dir(s.Dir), "go.work"))
	if err != nil || !strings.Contains(string(work), "use ./app") {
		t.Fatalf("go.work: %q, %v", work, err)
	}
	s.Press(term.KeyF9).Awaits("Build succeeded", patience)
}

// fakeBazel is a stand-in for Bazel: asked for the targets it names three,
// and asked for anything else it says what it was asked.
func fakeBazel(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bazel")
	script := "#!/bin/sh\nif [ \"$1\" = query ]; then\n  echo 'Loading: 2 packages loaded'\n  echo 'go_binary rule //tools/hello:hello'\n" +
		"  echo 'go_library rule //tools/hello:hello_lib'\n  echo 'go_test rule //tools/hello:hello_test'\n  echo 'go_binary rule //elsewhere:other'\n  exit 3\nfi\n" +
		"echo \"bazel-was-asked: $*\"\necho 'tools/hello/main.go:3:1: an error Bazel names from the workspace root'\n[ \"$1\" = build ] && exit 1\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInABazelWorkspaceTheKeysBuildWithBazelAndATargetIsFoundByTyping(t *testing.T) {
	files := map[string]string{
		"MODULE.bazel":        "module(name = \"ws\")\n",
		"tools/hello/BUILD":   "go_binary(name = \"hello\")\n",
		"tools/hello/go.mod":  "module example.com/hello\n\ngo 1.24\n",
		"tools/hello/main.go": "package main\n\nfunc main() {}\n",
		"elsewhere/BUILD":     "",
		"elsewhere/other.go":  "package main\n",
	}
	s := stack.Start(t, files, stack.Options{Sub: "tools/hello", Bazel: fakeBazel(t)}).Open("main.go")
	s.Sees("═ hello ═", "╚═ bazel ═")
	s.Press(term.KeyF9).Awaits("Build failed", patience)
	s.Sees("bazel build //tools/hello/...", "bazel-was-asked: build //tools/hello/...", " main.go ", "╚═ 3:1 ═")
	s.Ctrl('e').Awaits("Bazel targets", patience).Sees("▶ //tools/hello:hello  go_binary", "◆ //tools/hello:hello_test  go_test", "▶ //elsewhere:other  go_binary")
	s.Type("hello:hello").Press(term.KeyEnter).Sees("Run", "Open its BUILD file").Press(term.KeyEnter)
	if out := s.Out.String(); !strings.Contains(out, "bazel-was-asked: run //tools/hello:hello") {
		t.Fatalf("the target was not run on the terminal:\n%s", out)
	}
	s.Chord(term.KeyF9, term.Ctrl)
	if out := s.Out.String(); strings.Count(out, "bazel-was-asked: run //tools/hello:hello") != 2 {
		t.Fatalf("Ctrl+F9 did not run the remembered target:\n%s", out)
	}
	s.Ctrl('e').Type("hello_lib").Press(term.KeyEnter).Type("open").Press(term.KeyEnter).Sees(" BUILD ", "go_binary(name = \"hello\")")
}
