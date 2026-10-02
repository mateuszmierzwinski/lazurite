package ide

import "errors"

// What the IDE refuses, in words the status line can show.
var (
	// ErrNoTarget is Run or Debug with no program to start.
	ErrNoTarget = errors.New("no program to run: open a file of package main, or set Run → Target")
	// ErrUnnamed is an action that needs a file on disk, on a text never
	// saved.
	ErrUnnamed = errors.New("save the file first")
	// ErrTooManyDocuments is one text too many.
	ErrTooManyDocuments = errors.New("too many files are open")
	// ErrNotEmpty is a new project asked for where something already is.
	ErrNotEmpty = errors.New("that directory already holds files: a new project needs an empty or a new one")
	// ErrBusy is a second job while one is running.
	ErrBusy = errors.New("a job is already running: Build → Stop ends it")
)
