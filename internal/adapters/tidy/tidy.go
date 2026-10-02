// Package tidy cleans a Go source up: formats it and makes its imports
// match what it uses — the missing ones added, the unused ones removed.
//
// It does it in this process, with the same package `goimports` is made
// of, so there is no tool to install and none to run.
package tidy

import (
	"go/format"
	"strings"

	"golang.org/x/tools/imports"

	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
)

// Cleaner cleans Go sources up.
type Cleaner struct {
	options *imports.Options
}

// New is the cleaner as the IDE's port.
func New() ide.Tidier {
	return Cleaner{options: &imports.Options{Comments: true, TabIndent: true, TabWidth: 8}}
}

// Tidy is a source cleaned up. The path says where the file lives, which
// is how the imports it could mean are found: in its own module first.
//
// A source that does not parse is refused with the parser's first words;
// one whose imports cannot be worked out — no Go tool, no module — is
// still formatted.
func (c Cleaner) Tidy(path, text string) (string, error) {
	out, err := imports.Process(path, []byte(text), c.options)
	if err == nil {
		return string(out), nil
	}
	formatted, formatErr := format.Source([]byte(text))
	if formatErr != nil {
		return "", firstError(formatErr)
	}
	return string(formatted), nil
}

// syntaxError is a parser's complaint, cut to its first.
type syntaxError struct{ text string }

func (e syntaxError) Error() string { return e.text }

// firstError is an error cut to its first line: a parser lists every
// fault it found, and the first is the one to fix.
func firstError(err error) error {
	first, _, _ := strings.Cut(err.Error(), "\n")
	return syntaxError{text: first}
}
