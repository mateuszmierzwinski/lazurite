// Package bazeltarget is what a Bazel workspace can build, test and run, and
// how an operator finds one by typing — the thing `basalt` does in a terminal.
//
// It is the editors' shared copy of the shape basalt indexes, not an import of
// it: basalt's domain packages are private to basalt
// (//src/go/tools/basalt:__subpackages__), and an application reaching into a
// CLI tool's internals would couple two things that ship separately. The RULES
// are basalt's, deliberately — the same `label_kind` parsing, the same literal
// or regular-expression search — so they all agree about what a target is.
//
// It is pure: it reads what `bazel query` printed and decides. Running bazel
// is an adapter's business, and what a target's verb becomes — a run
// configuration, a job — is each editor's own.
package bazeltarget

// Kind is a Bazel rule kind — "go_binary", "cc_test". It decides what doing a
// target means (VerbOf), so it is carried rather than re-derived.
type Kind string

// Target is one rule, as `bazel query --output=label_kind` reports it.
type Target struct {
	Kind    Kind
	Package string // "//src/go/apps/lazurite/cmd/lazurite"
	Name    string // "lazurite"
	Label   string // "//src/go/apps/lazurite/cmd/lazurite:lazurite"
	Display string // "[lazurite:lazurite]" — basalt's short form
}

// Listing is one query's answer.
//
// Home is the open project's package path within the workspace
// ("src/go/apps/lazurite"), or empty where it could not be told — a remote
// workspace, a project that is the workspace root. It is what puts the
// project's own targets first in a monorepo of thousands.
type Listing struct {
	Targets []Target
	Home    string
}

// Verb is what doing a target means: run a binary, test a test, build the rest.
type Verb string

// The verbs, spelled as the bazel subcommands they are.
const (
	VerbRun   Verb = "run"
	VerbTest  Verb = "test"
	VerbBuild Verb = "build"
)

// ExitPartial is bazel's exit code for "finished, with errors" under
// --keep_going: the targets it could load are still printed.
const ExitPartial = 3
