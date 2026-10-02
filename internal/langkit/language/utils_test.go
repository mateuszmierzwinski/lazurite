package language

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtensionLowerCasesAndKeepsTheDot(t *testing.T) {
	for path, want := range map[string]string{
		"/a/b/main.go":    ".go",
		"/a/b/MAIN.GO":    ".go",
		"relative/x.RS":   ".rs",
		"/a/b/.hidden.py": ".py",
	} {
		if got := Extension(path); got != want {
			t.Fatalf("Extension(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestExtensionIsEmptyWhereThereIsNone(t *testing.T) {
	for _, path := range []string{"/a/b/Makefile", "LICENCE", "", "/a/b/", "/a/b/trailing."} {
		if got := Extension(path); got != "" {
			t.Fatalf("Extension(%q) = %q, want the empty string", path, got)
		}
	}
}

func TestNormaliseExtensionForgivesHowItWasWritten(t *testing.T) {
	for _, written := range []string{".go", "go", ".GO", "  .Go  ", "GO"} {
		if got := NormaliseExtension(written); got != ".go" {
			t.Fatalf("NormaliseExtension(%q) = %q, want %q", written, got, ".go")
		}
	}
}

func TestNormaliseExtensionRefusesWhatIsNotOne(t *testing.T) {
	for _, written := range []string{"", "   ", ".", " . "} {
		if got := NormaliseExtension(written); got != "" {
			t.Fatalf("NormaliseExtension(%q) = %q, want the empty string", written, got)
		}
	}
}

func TestLanguageIDNamesTheProtocolsIdentifier(t *testing.T) {
	for path, want := range map[string]string{
		"a/main.go": "go", "a/lib.rs": "rust", "a/x.TS": "typescript",
		"a/BUILD.bzl": "starlark", "a/notes.md": "markdown",
	} {
		if got := LanguageID(path); got != want {
			t.Fatalf("LanguageID(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestLanguageIDFallsBackToPlaintext(t *testing.T) {
	for _, path := range []string{"a/Makefile", "a/x.wibble", ""} {
		if got := LanguageID(path); got != "plaintext" {
			t.Fatalf("LanguageID(%q) = %q, want plaintext", path, got)
		}
	}
}

func TestConfinedAcceptsAPathInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "pkg", "file.go")
	got, err := Confined(root, inside)
	if err != nil {
		t.Fatalf("Confined refused a path inside the root: %v", err)
	}
	if got != inside {
		t.Fatalf("Confined = %q, want %q", got, inside)
	}
	if _, err := Confined(root, root); err != nil {
		t.Fatalf("Confined refused the root itself: %v", err)
	}
}

func TestConfinedRefusesAPathOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		"/etc/shadow",
		filepath.Join(root, "..", "elsewhere", "x.go"),
		filepath.Join(root, "..", filepath.Base(root)+"-sibling", "x.go"),
	} {
		if _, err := Confined(root, path); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("Confined(%q) error = %v, want ErrOutsideRoot", path, err)
		}
	}
}

func BenchmarkExtension(b *testing.B) {
	for b.Loop() {
		_ = Extension("/home/user/project/internal/core/language/utils.go")
	}
}

func BenchmarkNormaliseExtension(b *testing.B) {
	for b.Loop() {
		_ = NormaliseExtension("  .Go  ")
	}
}

func BenchmarkLanguageID(b *testing.B) {
	for b.Loop() {
		_ = LanguageID("/home/user/project/main.go")
	}
}

func BenchmarkConfined(b *testing.B) {
	root := b.TempDir()
	target := filepath.Join(root, "pkg", "file.go")
	b.ResetTimer()
	for b.Loop() {
		_, _ = Confined(root, target)
	}
}

func TestConfinedRefusesASymlinkThatLeavesTheRoot(t *testing.T) {
	// The classic escape from every sandbox that only counted `..` segments: a
	// link inside the workspace pointing anywhere at all.
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("this filesystem does not do symlinks: %v", err)
	}
	if _, err := Confined(root, filepath.Join(link, "secret.txt")); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("Confined followed a symlink out of the workspace: %v", err)
	}
	// And the same through a leaf that does not exist yet, which is where a
	// lexical fallback would have let it through.
	if _, err := Confined(root, filepath.Join(link, "not", "created", "yet.go")); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("Confined followed a symlinked parent: %v", err)
	}
}

func TestResolvedKeepsAPathThatDoesNotExistYet(t *testing.T) {
	// A server may legitimately name a file about to be created.
	root := t.TempDir()
	wanted := filepath.Join(root, "generated", "file.go")
	got, err := Resolved(wanted)
	if err != nil {
		t.Fatalf("Resolved = %v", err)
	}
	if filepath.Base(got) != "file.go" {
		t.Fatalf("Resolved = %q, want it to end at file.go", got)
	}
	if _, err := Confined(root, wanted); err != nil {
		t.Fatalf("Confined refused a file about to be created: %v", err)
	}
}

func TestResolvedRefusesNothing(t *testing.T) {
	// Every input has an answer; the refusal belongs to Confined, which is the
	// one that knows what the workspace is.
	for _, path := range []string{"", ".", "/", "relative/x", "/nowhere/at/all"} {
		if _, err := Resolved(path); err != nil {
			t.Fatalf("Resolved(%q) = %v", path, err)
		}
	}
}

func BenchmarkResolved(b *testing.B) {
	root := b.TempDir()
	for b.Loop() {
		_, _ = Resolved(root)
	}
}
