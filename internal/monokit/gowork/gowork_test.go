package gowork

import (
	"path/filepath"
	"reflect"
	"testing"
)

// machine describes a filesystem as a set of files, so a test says what the
// world looks like rather than building one on disk.
type machine map[string]string

// look is the two questions Detached asks, answered from the map.
func (m machine) look() Look {
	return Look{
		ReadFile: func(path string) ([]byte, bool) {
			body, ok := m[filepath.Clean(path)]
			return []byte(body), ok
		},
		Exists: func(path string) bool {
			_, ok := m[filepath.Clean(path)]
			return ok
		},
	}
}

// The monorepo as it actually stood when this was reported: a workspace listing
// two projects, and a third beside them that it does not list.
const monorepoWork = `go 1.26.8

use (
	./src/go/apps/framework
	./src/go/lib/ollama
)
`

// theMonorepo is that tree, with both projects and the newcomer present.
func theMonorepo() machine {
	return machine{
		"/repo/go.work":                  monorepoWork,
		"/repo/src/go/lib/ollama/go.mod": "module ollama",
		"/repo/src/go/webapp/go.mod":     "module webapp",
	}
}

func TestAProjectTheWorkspaceDoesNotListIsDetached(t *testing.T) {
	// The operator's own case (2026-09-24): a new project under a monorepo
	// whose go.work had not been extended to it yet. `go list ./...` in that
	// directory answers "directory prefix . does not contain modules listed in
	// go.work", and gopls inherits it — which presents as no completion at all.
	env := Env{Root: "/repo/src/go/webapp"}
	if !Detached(env, theMonorepo().look()) {
		t.Fatal("a module the workspace does not list was not detached")
	}
}

func TestAProjectTheWorkspaceListsKeepsItsWorkspace(t *testing.T) {
	// The ordinary case, and the one that must not regress: a project inside
	// its workspace needs the workspace, because that is what resolves its
	// sibling modules. Turning GOWORK off here would break every monorepo
	// project lazurite opens.
	env := Env{Root: "/repo/src/go/lib/ollama"}
	if Detached(env, theMonorepo().look()) {
		t.Fatal("a listed module was detached from its own workspace")
	}
}

func TestTheDecisionIsSkippedForEveryCaseItDoesNotApplyTo(t *testing.T) {
	for _, row := range []struct {
		why string
		env Env
		m   machine
	}{
		{"no project is open", Env{Root: ""}, theMonorepo()},
		{"the operator set GOWORK themselves",
			Env{Root: "/repo/src/go/webapp", GOWORK: "/repo/go.work"}, theMonorepo()},
		{"the folder is not a module at all",
			Env{Root: "/repo/src/go/notes"}, theMonorepo()},
		{"no workspace encloses it",
			Env{Root: "/elsewhere/app"}, machine{"/elsewhere/app/go.mod": "module app"}},
	} {
		if Detached(row.env, row.m.look()) {
			t.Errorf("detached although %s", row.why)
		}
	}
}

func TestTheNEARESTWorkspaceIsTheOneThatDecides(t *testing.T) {
	// A module with a go.work of its own inside a larger workspace answers to
	// its own, exactly as the go command does — it stops at the first one it
	// finds walking up.
	m := machine{
		"/repo/go.work":       "use ./other\n",
		"/repo/inner/go.work": "use .\n",
		"/repo/inner/go.mod":  "module inner",
	}
	if Detached(Env{Root: "/repo/inner"}, m.look()) {
		t.Fatal("a module listed by its own nearest workspace was detached")
	}
}

func TestAUseEntryIsReadInEverySpellingTheToolchainWrites(t *testing.T) {
	for _, row := range []struct {
		body string
		want []string
		why  string
	}{
		{"use ./a\n", []string{"./a"}, "the bare form"},
		{"use (\n\t./a\n\t./b\n)\n", []string{"./a", "./b"}, "the parenthesised block"},
		{"use(\n./a\n)\n", []string{"./a"}, "a block with no space before the bracket"},
		{"use \"./with space\"\n", []string{"./with space"}, "a quoted path"},
		{"use ./a // the app\n", []string{"./a"}, "a trailing comment"},
		{"// use ./a\n", nil, "a commented-out entry is not an entry"},
		{"go 1.26.8\n", nil, "a file that uses nothing"},
	} {
		if got := Uses(row.body); !reflect.DeepEqual(got, row.want) {
			t.Errorf("Uses(%q) = %v, want %v — %s", row.body, got, row.want, row.why)
		}
	}
}

func TestOneDirectorySpelledThreeWaysIsOneModule(t *testing.T) {
	// An entry is written relative to the workspace and the spelling varies;
	// compared as text rather than as a cleaned path, a listed module reads as
	// unlisted and the workspace is turned off under a project that needs it.
	for _, spelling := range []string{"./app", "app", "./app/", "./x/../app"} {
		m := machine{"/repo/go.work": "use " + spelling + "\n", "/repo/app/go.mod": "module app"}
		if Detached(Env{Root: "/repo/app"}, m.look()) {
			t.Errorf("the entry %q did not match the directory it names", spelling)
		}
	}
}

func TestADecisionWithNoFilesystemIsNoDecision(t *testing.T) {
	// A zero Look must not panic and must not detach: a missing dependency is
	// not evidence that a project is outside its workspace.
	if Detached(Env{Root: "/repo/app"}, Look{}) {
		t.Fatal("detached with nothing to read the machine with")
	}
}

func BenchmarkUses(b *testing.B) {
	for b.Loop() {
		Uses(monorepoWork)
	}
}

func BenchmarkDetached(b *testing.B) {
	env, look := Env{Root: "/repo/src/go/webapp"}, theMonorepo().look()
	for b.Loop() {
		Detached(env, look)
	}
}
