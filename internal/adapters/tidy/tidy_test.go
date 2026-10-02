package tidy

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
)

var _ ide.Tidier = Cleaner{}

func TestASourceIsFormatted(t *testing.T) {
	got, err := New().Tidy(filepath.Join(t.TempDir(), "a.go"), "package a\nfunc  F( ) int{\nreturn 1}\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "package a\n\nfunc F() int {\n\treturn 1\n}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestMissingImportsAreAddedAndUnusedOnesRemoved(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no Go tool here: imports cannot be worked out, only formatted")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module tidied\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := "package main\n\nimport \"os\"\n\nfunc main() {\n\tfmt.Println(strings.ToUpper(\"x\"))\n}\n"
	got, err := New().Tidy(filepath.Join(dir, "main.go"), source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "\"fmt\"") || !strings.Contains(got, "\"strings\"") || strings.Contains(got, "\"os\"") {
		t.Fatalf("got %q", got)
	}
}

func TestASourceThatDoesNotParseIsRefusedWithTheFirstFault(t *testing.T) {
	_, err := New().Tidy("a.go", "package a\n\nfunc {\n\nfunc }\n")
	if err == nil {
		t.Fatal("a source that does not parse was cleaned up")
	}
	if strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), "3:") {
		t.Fatalf("got %q", err.Error())
	}
}

func TestFirstErrorKeepsOneLine(t *testing.T) {
	if got := firstError(errors.New("one\ntwo")).Error(); got != "one" {
		t.Fatalf("got %q", got)
	}
}

func BenchmarkFirstError(b *testing.B) {
	err := errors.New("a.go:3:1: expected declaration\na.go:5:1: more")
	for b.Loop() {
		_ = firstError(err)
	}
}
