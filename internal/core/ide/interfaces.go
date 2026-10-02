package ide

import (
	"context"

	"github.com/mateuszmierzwinski/lazurite/internal/core/edit"
	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
)

// Terminal is the real terminal: where keys come from and frames go.
type Terminal interface {
	// Events is the keys, mouse, pastes and resizes, in order. It is closed
	// when the terminal is gone.
	Events() <-chan term.Event
	// Size is how many columns and rows the terminal has now.
	Size() (w, h int)
	// Show writes the bytes of a frame.
	Show(frame []byte) error
	// Lend hands the terminal — the user's own screen, cooked input — to
	// something else until it returns, then takes it back.
	Lend(use func()) error
}

// Files is the disk.
type Files interface {
	Read(path string) (string, error)
	Write(path, text string) error
	// List is a directory's entries, directories first.
	List(dir string) ([]Entry, error)
	// Sources is every source file under a root, as paths relative to it.
	Sources(root string) ([]string, error)
	// Project is the project a directory belongs to: the nearest directory
	// at or above it that holds a go.mod or a go.work, or itself.
	Project(dir string) string
}

// Tidier cleans a Go source up: formats it, adds the imports it uses and
// has not named, removes the ones it names and does not use.
type Tidier interface {
	Tidy(path, text string) (string, error)
}

// Intel is what knows the language: completions, definitions, what is
// wrong. Every call returns at once; an answer arrives later through the
// function it was asked with, on a goroutine of the implementation's own.
type Intel interface {
	// Listen names who is told what is wrong with a file.
	Listen(heard func(path string, problems []Problem))
	Open(path, text string)
	Change(path, text string)
	Save(path, text string)
	Close(path string)
	Complete(path string, at edit.Pos, done func([]Suggestion))
	Define(path string, at edit.Pos, done func(Place, bool))
	Describe(path string, at edit.Pos, done func(string))
	Stop()
}

// Runner runs the Go tool.
type Runner interface {
	// Capture runs a job and hands over what it prints, line by line, then
	// how it ended; both from a goroutine of the runner's own. The function
	// it returns stops the job.
	Capture(ctx context.Context, job Job, line func(string), done func(error)) (stop func())
	// Foreground runs a job on the terminal itself — it reads the keyboard
	// and writes the screen — and returns when the user has seen the end of
	// it. A job with no arguments only waits for the user.
	Foreground(ctx context.Context, job Job) error
}

// Command is one thing the IDE can be asked to do, from a menu or a key.
type Command interface {
	Run(a *App)
}
