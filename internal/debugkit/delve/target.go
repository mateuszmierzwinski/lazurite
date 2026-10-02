package delve

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// What a run configuration means to a debugger.
//
// The window hands this package whatever the operator chose to RUN — `go run
// .`, `go test ./...`, a built binary, or something that is neither — and a
// debugger needs one of two things: a Go package to build, or a program to
// start. Working that out is a rule and lives here, away from the ptrace.

// goSubcommands are the ones that name a package this package can build.
//
// `run`, `build` and `install` all point at a package. `test` does too, but a
// test binary is built differently (`go test -c`), and debugging one through
// this path would run the package's main instead — so it is deliberately absent
// until there is somewhere for the test build to go.
var goSubcommands = map[string]bool{"run": true, "build": true, "install": true}

// targetOf reads a run configuration as a debugging target.
//
// It answers the target, the arguments the DEBUGGEE gets, and whether the
// target is a package that has to be built first. A configuration that is
// neither — `bazel test //...`, `make`, a shell line — answers with an empty
// target, which Launch refuses with a message naming what it needed.
func targetOf(program string, args []string, dir string) (string, []string, bool) {
	if isGoToolchain(program) {
		return goTarget(args, dir)
	}
	// A bare command NAME is not a debugging target. `bazel`, `make`, `sh` are
	// all programs on PATH that run something else, and Delve pointed at one
	// would debug the runner rather than the program — so only a path is taken,
	// which is what a built binary in a project looks like.
	if !isPath(program) {
		return "", nil, false
	}
	return absoluteIn(dir, program), args, false
}

// isPath reports whether a command names a file rather than something on PATH.
func isPath(program string) bool {
	return program != "" && strings.ContainsRune(program, filepath.Separator)
}

// isGoToolchain reports whether a command is the Go tool itself.
func isGoToolchain(program string) bool {
	base := filepath.Base(program)
	return base == "go" || base == "go.exe"
}

// goTarget reads `go run .` and its neighbours.
//
// The package is the first argument that is not a FLAG, and `.` where there is
// none — which is what `go run` itself does. Everything after a bare `--` is
// the debuggee's, because that is where the Go tool stops reading arguments and
// the program starts.
func goTarget(args []string, dir string) (string, []string, bool) {
	rest, ok := afterSubcommand(args)
	if !ok {
		return "", nil, false
	}
	pkg, runArgs := ".", []string{}
	for i, arg := range rest {
		switch {
		case arg == "--":
			runArgs = append(runArgs, rest[i+1:]...)
			return absoluteIn(dir, pkg), runArgs, true
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			pkg = arg
			runArgs = append(runArgs, afterSeparator(rest[i+1:])...)
			return absoluteIn(dir, pkg), runArgs, true
		}
	}
	return absoluteIn(dir, pkg), runArgs, true
}

// afterSeparator is the debuggee's own arguments with the `--` that introduced
// them removed: it is the Go tool's punctuation, and passing it on would hand
// the program an argument nobody typed for it.
func afterSeparator(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

// afterSubcommand is the arguments following a `go` subcommand this package can
// debug, and whether there was one.
func afterSubcommand(args []string) ([]string, bool) {
	for i, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !goSubcommands[arg] {
			return nil, false
		}
		return args[i+1:], true
	}
	return nil, false
}

// absoluteIn resolves a package or a program against the configuration's
// working directory.
//
// A RELATIVE package is relative to the project — that is what makes a
// configuration written on one machine work on another — and Delve's build runs
// in whatever directory this window was started in, which is not it.
func absoluteIn(dir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") || path == "." {
		return filepath.Join(dir, path)
	}
	// An import path like `example.com/x/y` is not a directory and must be
	// left alone: joining it to the project would name a folder that does not
	// exist, and the build would fail saying so about the wrong thing.
	if looksLikeImportPath(path) {
		return path
	}
	return filepath.Join(dir, path)
}

// looksLikeImportPath reports whether a package is named by import path rather
// than by directory.
func looksLikeImportPath(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return strings.Contains(first, ".")
}

// pidText is this process's identifier, for a name no other window will build
// over.
func pidText() string { return strconv.Itoa(os.Getpid()) }
