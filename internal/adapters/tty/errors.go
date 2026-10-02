package tty

import "errors"

// ErrNotATerminal is input that is not a terminal: a pipe, a file.
var ErrNotATerminal = errors.New("standard input is not a terminal")
