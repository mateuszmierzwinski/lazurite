// Package compose builds the IDE from its real parts.
//
// It is half of the composition root. `cmd/lazurite/main.go` keeps what
// only a process has — flags, the real terminal, signals — and this
// package wires the adapters into the IDE's ports. It is separate so that
// the black-box suites build the IDE with the SAME function the binary
// calls: something wired here is wired in both, and something that is not
// is missing from both. Besides `cmd/`, this is the only package allowed
// to name a concrete adapter.
package compose

import (
	"context"
	"io"
	"os"

	"github.com/mateuszmierzwinski/lazurite/internal/adapters/gopls"
	"github.com/mateuszmierzwinski/lazurite/internal/adapters/gotool"
	"github.com/mateuszmierzwinski/lazurite/internal/adapters/osfiles"
	"github.com/mateuszmierzwinski/lazurite/internal/adapters/tidy"
	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/debug"
	"github.com/mateuszmierzwinski/lazurite/internal/debugkit/delve"
)

// Options is what differs between one IDE and another.
type Options struct {
	// Terminal is where the IDE is shown and its keys come from.
	Terminal ide.Terminal
	// Dir is where the IDE was started; the project is the nearest go.mod
	// at or above it.
	Dir string
	// Go is the Go tool; Gopls the language server, empty to do without;
	// Bazel the build tool of a Bazel workspace.
	Go, Gopls, Bazel string
	// Debugger says whether the built-in debugger is offered.
	Debugger bool
	// In, Out and Err are what a program run in the foreground reads and
	// writes.
	In       io.Reader
	Out, Err io.Writer
	// Version is shown in the About box.
	Version string
}

// Build wires the adapters into the IDE's ports and answers the IDE.
func Build(ctx context.Context, o Options) *ide.App {
	root := osfiles.Root(o.Dir)
	deps := ide.Deps{
		Terminal: o.Terminal,
		Files:    osfiles.New(),
		Tidier:   tidy.New(),
		Reroot:   func(to string) ide.Intel { return intel(ctx, o.Gopls, to) },
		Detach:   standalone(os.Getenv(workVariable)),
		Gowork:   os.Getenv(workVariable),
		Notice:   notice(o.Gopls),
		Warming:  o.Gopls != "" && gotool.Available(o.Gopls),
		Runner:   gotool.New(o.Go, o.Bazel, o.In, o.Out, o.Err),
		Root:     root,
		Version:  o.Version,
	}
	if o.Debugger {
		deps.NewDebugger = debugger
	}
	return ide.New(ctx, deps)
}

// intel is the language server when there is one to start, and nothing
// when there is not: the editor works either way.
func intel(ctx context.Context, command, root string) ide.Intel {
	if command == "" || !gotool.Available(command) {
		return ide.NoIntel()
	}
	return gopls.New(ctx, command, root)
}

// workVariable is the variable that tells the Go toolchain which go.work
// to use, or — set to "off" — to use none.
const workVariable = "GOWORK"

// standalone is what is done each time the IDE enters a project, with
// whether that project is a module its go.work does not list.
//
// Such a module is refused by the whole toolchain ("directory prefix . does
// not contain modules listed in go.work"), and by everything built on it:
// the language server, the build, the clean-up, the debugger's own build.
// They are all started from this process, so the answer is given here, in
// this process's environment, and they all inherit it. Leaving such a
// project puts back what the user had.
func standalone(original string) func(detached bool) {
	return func(detached bool) {
		switch {
		case detached:
			_ = os.Setenv(workVariable, "off")
		case original == "":
			_ = os.Unsetenv(workVariable)
		default:
			_ = os.Setenv(workVariable, original)
		}
	}
}

// notice is what the status line says at the start when the machine has
// no language server: the one thing whose absence is otherwise only
// noticed as completion not working.
func notice(command string) string {
	if command == "" || gotool.Available(command) {
		return ""
	}
	return command + " not found — completion and live problems are off (go install golang.org/x/tools/gopls@latest)"
}

// debugger is Delve, set up for a console: the program is held until its
// breakpoints are in, and what it prints is caught rather than written
// over the screen.
func debugger(w debug.Watcher) debug.Debugger {
	return delve.New(w, delve.Held(), delve.Captured())
}
