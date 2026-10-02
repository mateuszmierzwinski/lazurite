// Package gowork decides whether the open project is a Go module that the
// workspace above it does not list.
//
// The Go toolchain walks UP from a directory looking for `go.work`, and a
// module it finds that way but which the file does not `use` is refused
// outright: `directory prefix . does not contain modules listed in go.work`.
// Every tool built on the toolchain inherits that, gopls included — so a
// project opened in this state has no completion, no diagnostics, no hover and
// no refactorings, and the editor looks broken rather than the workspace.
//
// This is not hypothetical. The operator opened a new project under a monorepo
// whose `go.work` did not list it yet (2026-09-24) and reported the symptom the
// only way it presents: typing a receiver and a dot offered nothing.
//
// The answer is `GOWORK=off` for that server, which is what the toolchain calls
// a standalone module. It is decided here, with the filesystem injected, for
// the reason `toolpath` decides its search order here: a rule is testable
// without a filesystem, and where a file lives is an adapter's business.
package gowork

import (
	"path/filepath"
	"strings"
)

// workFile is the name the toolchain looks for on its way up.
const workFile = "go.work"

// modFile marks a directory as a module, which is what makes the question
// meaningful at all.
const modFile = "go.mod"

// Detached reports whether the project is a module outside the workspace that
// encloses it.
//
// FALSE for every ordinary case, and deliberately so — a project inside its
// workspace must keep the workspace, because that is what resolves its sibling
// modules. It answers false when the operator set GOWORK themselves, when the
// project is not a module, when no workspace encloses it, and when the
// workspace lists it.
func Detached(env Env, look Look) bool {
	if env.Root == "" || env.GOWORK != "" || look.ReadFile == nil || look.Exists == nil {
		return false
	}
	if !look.Exists(filepath.Join(env.Root, modFile)) {
		return false
	}
	dir, found := enclosingWorkspace(env.Root, look)
	if !found {
		return false
	}
	body, ok := look.ReadFile(filepath.Join(dir, workFile))
	if !ok {
		return false
	}
	return !lists(dir, string(body), env.Root)
}

// enclosingWorkspace is the nearest directory at or above the project holding a
// `go.work`, which is the search the go command itself performs.
func enclosingWorkspace(root string, look Look) (string, bool) {
	for dir := root; ; {
		if look.Exists(filepath.Join(dir, workFile)) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// lists reports whether a workspace uses the project directory.
//
// Compared as CLEANED ABSOLUTE PATHS rather than as the text in the file: an
// entry is written relative to the workspace and may carry `./`, a trailing
// separator or a `..`, and three spellings of one directory must not read as
// three different modules.
func lists(dir, body, root string) bool {
	want := filepath.Clean(root)
	for _, used := range Uses(body) {
		if filepath.Clean(filepath.Join(dir, used)) == want {
			return true
		}
	}
	return false
}

// Uses is every module directory a `go.work` body names.
//
// The file has two spellings — a bare `use ./x` and a parenthesised block — and
// both are read here rather than by the go command, because asking the command
// means running it, and running it in a directory it refuses is the very
// failure being diagnosed.
func Uses(body string) []string {
	var out []string
	block := false
	for _, raw := range strings.Split(body, "\n") {
		block, out = useLine(strings.TrimSpace(stripComment(raw)), block, out)
	}
	return out
}

// useLine reads one line of a `go.work`, carrying the block state through it.
func useLine(line string, block bool, out []string) (bool, []string) {
	switch {
	case line == "":
		return block, out
	case block && line == ")":
		return false, out
	case block:
		return block, append(out, unquote(line))
	case line == "use(" || line == "use (":
		return true, out
	case strings.HasPrefix(line, "use "):
		return block, append(out, unquote(strings.TrimSpace(line[len("use "):])))
	}
	return block, out
}

// stripComment drops a line comment, which `go.work` permits anywhere.
func stripComment(line string) string {
	if at := strings.Index(line, "//"); at >= 0 {
		return line[:at]
	}
	return line
}

// unquote takes the quotes off a path that has them. The toolchain writes a
// path containing a space that way, and a quoted entry compared with its quotes
// matches no directory at all.
func unquote(path string) string {
	if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
		return path[1 : len(path)-1]
	}
	return path
}
