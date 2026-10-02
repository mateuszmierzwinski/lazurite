package ide

import (
	"errors"
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
	"github.com/mateuszmierzwinski/lazurite/internal/monokit/bazeltarget"
)

const (
	mono = "/mono"
	app  = "/mono/src/app"
)

// workListing is a go.work that lists the app; workLeaving one that does
// not.
const (
	workListing = "go 1.26\n\nuse (\n\t./src/app\n\t./src/other\n)\n"
	workLeaving = "go 1.26\n\nuse (\n\t./src/other\n)\n"
)

// monorepo is a Bazel workspace with a go.work, holding the app the IDE is
// started in and a neighbour.
func monorepo(t *testing.T, work, gowork string) *rig {
	t.Helper()
	return startIn(t, app, gowork, map[string]string{
		mono + "/MODULE.bazel":        "module(name = \"mono\")\n",
		mono + "/go.work":             work,
		app + "/go.mod":               "module example.com/app\n",
		app + "/BUILD":                "go_binary(name = \"app\")\n",
		app + "/main.go":              mainSource,
		app + "/pkg/x.go":             "package pkg\n",
		app + "/pkg/BUILD.bazel":      "go_library(name = \"pkg\")\n",
		mono + "/src/other/BUILD":     "",
		mono + "/src/other/go.mod":    "module example.com/other\n",
		mono + "/src/other/other.go":  "package other\n",
		mono + "/src/other/README.md": "other\n",
	})
}

// queryOutput is what Bazel prints when asked for the workspace's targets.
const queryOutput = `Loading: 3 packages loaded
go_binary rule //src/app:app
go_library rule //src/app:app_lib
go_library rule //src/app/pkg:pkg
go_test rule //src/app/pkg:pkg_test
go_binary rule //src/other:other
go_test rule //src/other:other_test`

// answer makes the fake Bazel print its targets and end.
func (r *rig) answer(printed string, err error) *rig {
	for _, line := range strings.Split(printed, "\n") {
		r.runner.line(line)
	}
	r.runner.done(err)
	r.app.Drain()
	return r
}

// lastJob is the job most lately started.
func (r *rig) lastJob() string {
	job := r.runner.jobs[len(r.runner.jobs)-1]
	return job.Tool + "|" + job.Dir + "|" + strings.Join(job.Args, " ")
}

func TestAPlainGoModuleIsBuiltWithTheGoToolAndSaysSo(t *testing.T) {
	r := start(t)
	r.sees("╚═ go ═")
	if r.app.bazel() || r.app.proj.detached || r.app.standing() != "Go module" {
		t.Fatalf("got %+v", r.app.proj)
	}
	if len(r.detached) != 1 || r.detached[0] {
		t.Fatalf("the tools were told %v", r.detached)
	}
	r.ctrl('e').sees("This project is not in a Bazel workspace")
	r.app.Do("targets.refresh")
	r.sees("This project is not in a Bazel workspace")
	r.app.Do("work.add")
	r.sees("There is no go.work above this project that leaves it out")
}

func TestInABazelWorkspaceTheBuildAndTestKeysUseBazel(t *testing.T) {
	r := monorepo(t, workListing, "")
	r.sees("═ app ═", "╚═ bazel ═")
	if got := r.app.standing(); got != "Bazel //src/app" {
		t.Fatalf("got %q", got)
	}
	r.open("../../../mono/src/app/main.go")
	r.press(term.KeyF9)
	if got := r.lastJob(); got != "bazel|/mono|build //src/app/..." {
		t.Fatalf("F9 ran %s", got)
	}
	r.sees("bazel build //src/app/...   in /mono")
	r.runner.line("src/app/main.go:6:2: undefined: fmt.Printn")
	r.runner.done(errors.New("exit status 1"))
	r.app.Drain()
	r.sees(" main.go ", "╚═ 6:2 ═", "Build failed")
	if r.app.doc().path != app+"/main.go" {
		t.Fatalf("a place Bazel named was opened as %q — it names them from the workspace's root", r.app.doc().path)
	}
	if err := r.app.Open(app + "/pkg/x.go"); err != nil {
		t.Fatal(err)
	}
	r.chord(term.KeyF9, term.Alt)
	if got := r.lastJob(); got != "bazel|/mono|build //src/app/pkg:all" {
		t.Fatalf("Alt+F9 ran %s", got)
	}
	r.runner.done(nil)
	r.app.Drain()
	r.chord(term.KeyF9, term.Shift)
	if got := r.lastJob(); got != "bazel|/mono|test //src/app/pkg:all" {
		t.Fatalf("Shift+F9 ran %s", got)
	}
	r.runner.done(nil)
	r.app.Drain()
	r.app.Do("test")
	if got := r.lastJob(); got != "bazel|/mono|test //src/app/..." {
		t.Fatalf("Test all ran %s", got)
	}
	r.runner.done(nil)
	r.app.Drain()
	r.app.Do("vet")
	if got := r.lastJob(); got != "|/mono/src/app|vet ./..." {
		t.Fatalf("Vet, which Bazel does not do, ran %s", got)
	}
}

func TestATargetIsFoundByTypingAndDoneAsItsKindMeans(t *testing.T) {
	r := monorepo(t, workListing, "")
	r.open("../../../mono/src/app/main.go")
	r.ctrl('e').sees("Asking Bazel what the workspace holds")
	if got := r.lastJob(); got != "bazel|/mono|query kind(rule, //...:*) --output=label_kind --keep_going" {
		t.Fatalf("asked with %s", got)
	}
	r.answer(queryOutput, nil)
	r.sees("Bazel targets", "▶ //src/app:app  go_binary", "◆ //src/app/pkg:pkg_test  go_test", "· //src/app:app_lib  go_library", "▶ //src/other:other  go_binary")
	screen := r.screen()
	if strings.Index(screen, "//src/app/pkg:pkg_test") > strings.Index(screen, "//src/other:other ") {
		t.Fatal("the project's own targets are not listed before a neighbour's")
	}
	r.write("_test$").sees("◆ //src/app/pkg:pkg_test", "◆ //src/other:other_test").lacks("go_binary", "go_library")
	r.press(term.KeyEnter).sees("//src/app/pkg:pkg_test", "Test", "Run", "Build", "Use for Ctrl+F9", "Open its BUILD file")
	r.press(term.KeyEnter)
	if got := r.lastJob(); got != "bazel|/mono|test //src/app/pkg:pkg_test" {
		t.Fatalf("the test target was done as %s", got)
	}
	r.runner.done(nil)
	r.app.Drain()
	r.ctrl('e').sees("Bazel targets")
	if strings.Count(strings.Join(jobArgs(r), "\n"), "query") != 1 {
		t.Fatal("Bazel was asked for its targets a second time")
	}
	r.write("other:other ").press(term.KeyBackspace, term.KeyEnter).write("build").press(term.KeyEnter)
	if got := r.lastJob(); got != "bazel|/mono|build //src/other:other" {
		t.Fatalf("Build was done as %s", got)
	}
}

func jobArgs(r *rig) []string {
	var out []string
	for _, job := range r.runner.jobs {
		out = append(out, strings.Join(job.Args, " "))
	}
	return out
}

func TestRunInABazelWorkspaceRunsTheChosenTargetAndRemembersIt(t *testing.T) {
	r := monorepo(t, workListing, "")
	r.open("../../../mono/src/app/main.go")
	r.chord(term.KeyF9, term.Ctrl).answer(queryOutput, nil).sees("Bazel targets")
	r.write("app:app").press(term.KeyEnter).sees("Run").press(term.KeyEnter)
	if len(r.runner.foreground) != 1 || r.terminal.lent != 1 {
		t.Fatal("the target was not run on the terminal")
	}
	if job := r.runner.foreground[0]; job.Tool != ToolBazel || job.Dir != mono || strings.Join(job.Args, " ") != "run //src/app:app" {
		t.Fatalf("ran %+v", job)
	}
	r.app.Do("arguments")
	r.write("-v out.txt").press(term.KeyEnter).ctrl('r')
	if job := r.runner.foreground[1]; strings.Join(job.Args, " ") != "run //src/app:app -- -v out.txt" {
		t.Fatalf("Run again ran %+v", job)
	}
	r.app.Do("target")
	r.sees("Bazel targets")
	r.write("other:other").press(term.KeyDown, term.KeyUp, term.KeyEnter)
	r.write("use for").press(term.KeyEnter).sees("Ctrl+F9 runs //src/other:other")
	r.ctrl('r')
	if job := r.runner.foreground[2]; job.Args[1] != "//src/other:other" {
		t.Fatalf("ran %+v", job)
	}
	r.ctrl('e').write("app_lib").press(term.KeyEnter).write("open").press(term.KeyEnter).sees(" BUILD ", "go_binary(name = \"app\")")
	r.ctrl('e').write("pkg:pkg ").press(term.KeyBackspace, term.KeyEnter).write("open").press(term.KeyEnter).sees(" pkg/BUILD.bazel ")
	r.app.openBuildFile(bazeltarget.Target{Package: "//src/nowhere"})
	r.sees("No BUILD file in")
	r.ctrl('e').press(term.KeyEsc).ctrl('e').press(term.KeyEnter, term.KeyEsc)
	r.app.act("no such action", bazeltarget.Target{})
}

func TestAQueryThatNamesNothingIsAFailureAndOneThatFailsWithTargetsIsAnAnswer(t *testing.T) {
	r := monorepo(t, workListing, "")
	r.ctrl('e').answer("ERROR: no such package 'x'", errors.New("exit status 7")).sees("Bazel named no targets: ERROR: no such package 'x'")
	r.ctrl('e').answer("", errors.New("exec: bazel: not found")).sees("Bazel named no targets: exec: bazel: not found")
	r.ctrl('e').answer("", nil).sees("it printed nothing")
	r.ctrl('e').answer(queryOutput+"\nERROR: package //broken could not be loaded", errors.New("exit status 3")).sees("Bazel targets", "//src/app:app")
	r.press(term.KeyEsc)
	r.app.Do("targets.refresh")
	r.app.Do("targets.refresh")
	r.sees("a job is already running")
	r.answer("go_binary rule //only:one", nil).sees("//only:one").lacks("//src/app:app")
}

func TestWhatBazelIsBelievedAboutIsBounded(t *testing.T) {
	r := monorepo(t, workListing, "")
	var many strings.Builder
	for i := 0; i < maxTargets+500; i++ {
		many.WriteString("go_library rule //p:t" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + "\n")
	}
	r.app.targetsListed(many.String(), nil)
	if len(r.app.targets) != maxTargets {
		t.Fatalf("%d targets are kept", len(r.app.targets))
	}
	r.press(term.KeyEsc).write(strings.Repeat("q", 10))
	r.ctrl('e')
	if got := r.app.dialog; got == nil {
		t.Fatal("the search was not put up")
	}
	for i := 0; i < bazeltarget.MaxQueryLength+10; i++ {
		r.write("z")
	}
	r.lacks("//p:t")
}

func TestAModuleItsWorkspaceLeavesOutIsBuiltStandaloneAndCanBeAdded(t *testing.T) {
	r := monorepo(t, workLeaving, "")
	if !r.app.proj.detached || len(r.detached) != 1 || !r.detached[0] {
		t.Fatalf("the tools were not told the module stands alone: %+v, %v", r.app.proj, r.detached)
	}
	r.sees("╚═ bazel · GOWORK=off ═")
	if got := r.app.standing(); got != "Bazel //src/app · outside go.work (built standalone)" {
		t.Fatalf("got %q", got)
	}
	r.app.Do("work.add")
	want := "go 1.26\n\nuse (\n\t./src/other\n\t./src/app\n)\n"
	if got := r.files.files[mono+"/go.work"]; got != want {
		t.Fatalf("go.work is %q", got)
	}
	if r.app.proj.detached || r.detached[len(r.detached)-1] || len(r.rerooted) != 1 {
		t.Fatalf("after adding: %+v, told %v, language server restarted %d times", r.app.proj, r.detached, len(r.rerooted))
	}
	r.sees("Added to", "╚═ bazel ═")
}

func TestAGoWorkTheUserChoseThemselvesIsNotOverridden(t *testing.T) {
	r := monorepo(t, workLeaving, "/somewhere/else/go.work")
	if r.app.proj.detached || r.detached[0] {
		t.Fatal("the user's own GOWORK was overridden")
	}
}

func TestAGoWorkThatCannotBeChangedSaysSo(t *testing.T) {
	r := monorepo(t, workLeaving, "")
	r.files.readOnly[mono+"/go.work"] = true
	r.app.Do("work.add")
	r.sees("read-only")
	if !r.app.proj.detached {
		t.Fatal("a project that was not added is taken as added")
	}
	delete(r.files.files, mono+"/go.work")
	r.app.Do("work.add")
	r.sees("go.work could not be read")
}

func TestANewProjectInsideAMonorepoIsMadeTheMonoreposWay(t *testing.T) {
	r := monorepo(t, workListing, "")
	r.app.newProject(mono+"/src/fresh one", "example.com/fresh")
	r.runner.done(errors.New("no go tool"))
	r.app.Drain()
	build := r.files.files[mono+"/src/fresh one/BUILD"]
	for _, want := range []string{"go_binary(\n    name = \"freshone\"", "embed = [\":freshone_lib\"]", "importpath = \"example.com/fresh\"", "srcs = [\"main.go\"]"} {
		if !strings.Contains(build, want) {
			t.Fatalf("the BUILD file lacks %q:\n%s", want, build)
		}
	}
	r.sees("═ fresh one ═", "Add this project to the go.work above it?")
	r.write("y")
	if got := r.files.files[mono+"/go.work"]; !strings.Contains(got, "\t./src/other\n\t./src/fresh one\n)") {
		t.Fatalf("go.work is %q", got)
	}
	r.sees("╚═ bazel ═", " main.go ")
	r.app.newProject(mono+"/src/second", "example.com/second")
	r.files.files[mono+"/src/second/go.mod"] = "module example.com/second\n"
	r.runner.done(nil)
	r.app.Drain()
	r.write("n").sees("╚═ bazel · GOWORK=off ═")
	if strings.Contains(r.files.files[mono+"/go.work"], "second") {
		t.Fatal("No added the project to go.work")
	}
	r.app.offerWork()
	plain := start(t)
	plain.app.newProject("/work/solo", "solo")
	plain.runner.done(nil)
	plain.app.Drain()
	if _, made := plain.files.files["/work/solo/BUILD"]; made || plain.app.dialog != nil {
		t.Fatal("a project outside any workspace was given a BUILD file or asked about a go.work")
	}
}

func TestAUseIsAddedInsideTheBlockOrAsALineOfItsOwn(t *testing.T) {
	for body, want := range map[string]string{
		"go 1.26\n\nuse (\n\t./a\n)\n":      "go 1.26\n\nuse (\n\t./a\n\t./b\n)\n",
		"go 1.26\n\nuse ./a\n":              "go 1.26\n\nuse ./a\n\nuse ./b\n",
		"go 1.26\n":                         "go 1.26\n\nuse ./b\n",
		"use(\n)\nreplace (\n\tx => y\n)\n": "use(\n\t./b\n)\nreplace (\n\tx => y\n)\n",
		"":                                  "\n\nuse ./b\n",
	} {
		if got := withUse(body, "./b"); got != want {
			t.Errorf("withUse(%q) = %q, want %q", body, got, want)
		}
	}
}

func TestATargetsNameAndABuildFileHoldOnlyWhatALabelMay(t *testing.T) {
	for name, want := range map[string]string{"my app": "myapp", "tool-2.x_y": "tool-2.x_y", "\"; rm": "rm", "żółw": "w", "!!!": "app", "": "app"} {
		if got := targetName(name); got != want {
			t.Errorf("targetName(%q) = %q, want %q", name, got, want)
		}
	}
	got := buildFile("a\"b", "example.com/x\"\n)")
	if !strings.Contains(got, `importpath = "example.com/x\"\n)"`) || strings.Count(got, "go_binary(") != 1 || strings.Count(got, "\n)\n") != 2 {
		t.Fatalf("a hostile module path broke out of its string:\n%s", got)
	}
}

func TestTheLastLineOfAFailureIsWhatIsSaid(t *testing.T) {
	if lastLine("a\nb\n\n", nil) != "b" || lastLine("", errors.New("gone")) != "gone" || lastLine(" ", nil) != "it printed nothing" {
		t.Fatal("lastLine")
	}
	if targetLine(bazeltarget.Target{Kind: "go_test", Label: "//a:b"}) != "◆ //a:b  go_test" {
		t.Fatal("targetLine")
	}
}

func TestMovingToAnotherProjectSurveysItAfresh(t *testing.T) {
	r := monorepo(t, workListing, "")
	r.ctrl('e').answer(queryOutput, nil).press(term.KeyEnter, term.KeyEnter)
	r.files.files["/work/plain/go.mod"] = "module plain\n"
	r.app.moveTo("/work/plain", "")
	if r.app.bazel() || r.app.listed || r.app.runTarget != "" {
		t.Fatalf("the old workspace came along: %+v", r.app.proj)
	}
	r.sees("Project: /work/plain — Go module", "╚═ go ═")
	r.files.files["/work/notes/a.txt"] = "x"
	r.app.moveTo("/work/notes", "")
	r.sees("no go.mod")
	lone := startIn(t, "/lone", "", map[string]string{"/lone/BUILD": "", "/lone/go.mod": "module lone\n"})
	if lone.app.bazel() {
		t.Fatal("a BUILD file with no workspace above it was taken for a Bazel project")
	}
}

func BenchmarkWithUse(b *testing.B) {
	for b.Loop() {
		withUse(workListing, "./src/new")
	}
}

func BenchmarkTargetName(b *testing.B) {
	for b.Loop() {
		targetName("my new project")
	}
}

func BenchmarkBuildFile(b *testing.B) {
	for b.Loop() {
		buildFile("tool", "example.com/tool")
	}
}

func BenchmarkTargetLine(b *testing.B) {
	t := bazeltarget.Target{Kind: "go_binary", Label: "//src/app:app"}
	for b.Loop() {
		targetLine(t)
	}
}

func BenchmarkLastLine(b *testing.B) {
	for b.Loop() {
		lastLine(queryOutput, nil)
	}
}
