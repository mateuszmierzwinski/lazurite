// Package buildkind decides what a project is built with.
//
// It is given the NAMES at a project's root rather than a path: deciding is a
// rule and reading a directory is a machine, and the rule is what is worth
// testing — with no fixture, no temporary directory and no disk (Rule 1.4).
// The caller does the reading.
//
// Operator's rule, 2026-09-11: a project with a `go.mod` and no `BUILD` file is
// a Go project. A project with both is a Bazel project — this monorepo is, and
// its go.mod files exist for the editor rather than for building.
package buildkind

// Kind is what a project is built with.
//
// Unknown is the zero value and a normal answer: a folder of notes, a data
// directory, somebody's home folder opened to read one file. An editor that
// treated "cannot tell" as an error would offer a Run control that fails.
type Kind string

// The kinds that can be told apart from the root of a project.
const (
	Unknown Kind = ""
	Bazel   Kind = "bazel"
	Go      Kind = "go"
)

// bazelNames are the files that make a directory Bazel's.
var bazelNames = []string{"BUILD", "BUILD.bazel", "MODULE.bazel", "WORKSPACE", "WORKSPACE.bazel", "WORKSPACE.bzlmod"}

// Of is what the entries at a project's root say it is built with.
//
// BUILD wins over go.mod deliberately, and the order is the whole rule: Bazel
// is the authority wherever it is present, so a repository carrying both is
// built the way its BUILD files say and not the way its modules would.
func Of(entries []string) Kind {
	at := make(map[string]bool, len(entries))
	for _, entry := range entries {
		at[entry] = true
	}
	for _, name := range bazelNames {
		if at[name] {
			return Bazel
		}
	}
	if at["go.mod"] {
		return Go
	}
	return Unknown
}
