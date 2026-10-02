// Command lazurite is a console IDE for Go: a text-mode desktop with menus
// and windows, an editor that completes, cleans up and imports as you
// write, and building, testing, running and debugging on function keys.
//
// This file is the process half of the composition root — flags, the real
// terminal, signals; `internal/compose` wires the adapters into the IDE.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mateuszmierzwinski/lazurite/internal/adapters/tty"
	"github.com/mateuszmierzwinski/lazurite/internal/compose"
	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
)

// version is the release; a release build sets it with -X.
var version = "0.1.0"

var (
	goCommand    = flag.String("go", "go", "the Go tool to build, test and run with")
	goplsCommand = flag.String("gopls", "gopls", "the language server for completion and problems; empty to do without")
	bazelCommand = flag.String("bazel", "bazel", "the Bazel to build, test and run with inside a Bazel workspace")
	noDebugger   = flag.Bool("no-debugger", false, "start without the built-in debugger")
	logPath      = flag.String("log", "", "write diagnostics to this file (they are discarded otherwise)")
	showVersion  = flag.Bool("version", false, "print the version and exit")
)

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [directory | file...]\n\n", filepath.Base(os.Args[0]))
		fmt.Fprintf(os.Stderr, "%s — a console IDE for Go.\n", ide.Brand)
		fmt.Fprintf(os.Stderr, "Opens the files named, in the project (the nearest go.mod) they belong to;\n")
		fmt.Fprintf(os.Stderr, "given a directory, or nothing, it opens that project with no file.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if *showVersion {
		fmt.Println(ide.Brand, version)
		return
	}
	if err := run(flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "lazurite:", err)
		os.Exit(1)
	}
}

// run builds the IDE and runs it until the user leaves.
func run(args []string) error {
	dir, files, err := whatToOpen(args)
	if err != nil {
		return err
	}
	closeLog, err := diagnostics(*logPath)
	if err != nil {
		return err
	}
	defer closeLog()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	keepInterrupts()
	screen, closeScreen := tty.Output(os.Stdout)
	defer closeScreen()
	terminal, restore, err := tty.Open(os.Stdin, screen)
	if err != nil {
		return err
	}
	defer restore()
	app := compose.Build(ctx, compose.Options{
		Terminal: terminal, Dir: dir, Go: *goCommand, Gopls: *goplsCommand, Bazel: *bazelCommand, Debugger: !*noDebugger,
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Version: "version " + version,
	})
	openAll(app, files)
	return app.Run()
}

// openAll opens the files named on the command line. One that cannot be
// opened is noted in the log and passed over: the others still open.
func openAll(app *ide.App, files []string) {
	for _, file := range files {
		if err := app.Open(file); err != nil {
			log.Printf("lazurite: %s: %v", filepath.Base(file), err)
		}
	}
}

// whatToOpen reads the arguments: a directory is the project to open, and
// anything else is a file to open in it. With no directory named, the
// project is the one the first file is in, or the one the IDE was started
// in.
func whatToOpen(args []string) (dir string, files []string, err error) {
	dir, err = startingDirectory()
	if err != nil {
		return "", nil, err
	}
	named := false
	for _, arg := range args {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return "", nil, err
		}
		if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
			dir, named = abs, true
			continue
		}
		files = append(files, abs)
	}
	if !named && len(files) > 0 {
		dir = filepath.Dir(files[0])
	}
	return dir, files, nil
}

// startingDirectory is where the IDE was started from. Under `bazel run`
// that is not the working directory — Bazel runs a program among its own
// files and says where the user was in BUILD_WORKING_DIRECTORY — so the
// process moves there first: the project, and every file named, is then
// found where the user meant.
func startingDirectory() (string, error) {
	if from := os.Getenv("BUILD_WORKING_DIRECTORY"); from != "" {
		if err := os.Chdir(from); err != nil {
			return "", err
		}
	}
	return os.Getwd()
}

// diagnostics points the log at a file, or at nothing: the screen belongs
// to the IDE, and a line written to it by a library would be drawn over
// the windows. It answers the function that closes the file.
func diagnostics(path string) (func(), error) {
	if path == "" {
		log.SetOutput(io.Discard)
		return nothingToClose, nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	log.SetOutput(f)
	return func() { _ = f.Close() }, nil
}

// nothingToClose is what closes a log that goes nowhere.
func nothingToClose() {
	// No file was opened.
}

// keepInterrupts makes Ctrl+C harmless to the IDE while a program it runs
// has the terminal: the interrupt is meant for that program, and reaches
// it, but the terminal sends it to the IDE too. The signal is caught and
// dropped rather than ignored, because an ignored signal stays ignored in
// the programs the IDE starts — and they could not be interrupted either.
func keepInterrupts() {
	// 1: a signal that finds the channel full is dropped, which is what
	// is wanted of all of them.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGINT)
}
