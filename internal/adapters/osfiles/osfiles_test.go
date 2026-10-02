package osfiles

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
)

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestATextIsReadAndWrittenBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	disk := New()
	if _, err := disk.Read(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a missing file answered %v", err)
	}
	if err := disk.Write(path, "package a\n"); err != nil {
		t.Fatal(err)
	}
	if got, err := disk.Read(path); err != nil || got != "package a\n" {
		t.Fatalf("got %q, %v", got, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != newFileMode {
		t.Fatalf("a new file was made with mode %v", info.Mode().Perm())
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 1 {
		t.Fatalf("writing left %d files behind", len(left))
	}
}

func TestWritingKeepsAFilesPermissionsAndWritesThroughALink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.sh")
	put(t, real, "old")
	if err := os.Chmod(real, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.sh")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symbolic links here")
	}
	if err := New().Write(link, "new"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatal("writing through a link replaced the link")
	}
	if got, _ := os.ReadFile(real); string(got) != "new" {
		t.Fatalf("the file behind the link holds %q", got)
	}
	if info, _ := os.Stat(real); info.Mode().Perm() != 0o750 {
		t.Fatalf("the file's mode became %v", info.Mode().Perm())
	}
}

func TestAWriteThatCannotHappenLeavesTheOldTextAndNoRubbish(t *testing.T) {
	disk := New()
	made := filepath.Join(t.TempDir(), "new", "pkg", "a.go")
	if err := disk.Write(made, "x"); err != nil {
		t.Fatalf("the directories above a new file were not made: %v", err)
	}
	if err := disk.Write(filepath.Join(made, "under-a-file.go"), "x"); err == nil {
		t.Fatal("a write under something that is a file succeeded")
	}
	dir := t.TempDir()
	put(t, filepath.Join(dir, "a.txt"), "old")
	if os.Getuid() == 0 {
		return
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	if err := disk.Write(filepath.Join(dir, "a.txt"), "new"); err == nil {
		t.Fatal("a write into a read-only directory succeeded")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "old" {
		t.Fatalf("the old text was lost: %q", got)
	}
}

func TestOnlyRegularTextFilesOfASaneSizeAreRead(t *testing.T) {
	dir := t.TempDir()
	disk := New()
	if _, err := disk.Read(dir); !errors.Is(err, ErrNotAFile) {
		t.Fatalf("a directory answered %v", err)
	}
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err == nil {
		if _, err := disk.Read(fifo); !errors.Is(err, ErrNotAFile) {
			t.Fatalf("a pipe answered %v — reading it would never return", err)
		}
	}
	put(t, filepath.Join(dir, "bin"), "ELF\x00\x01\x02")
	if _, err := disk.Read(filepath.Join(dir, "bin")); !errors.Is(err, ErrBinary) {
		t.Fatalf("a binary answered %v", err)
	}
	put(t, filepath.Join(dir, "latin1"), "caf\xe9")
	if _, err := disk.Read(filepath.Join(dir, "latin1")); !errors.Is(err, ErrBinary) {
		t.Fatalf("invalid UTF-8 answered %v", err)
	}
	big := filepath.Join(dir, "big")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxFile + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := disk.Read(big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("an oversized file answered %v", err)
	}
	if _, err := textOf(make([]byte, MaxFile+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatal("textOf took too many bytes")
	}
}

func TestADirectoryIsListedDirectoriesFirstWithoutHiddenNames(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "b.go"), "")
	put(t, filepath.Join(dir, "a.go"), "")
	put(t, filepath.Join(dir, ".hidden"), "")
	put(t, filepath.Join(dir, "zdir", "x.go"), "")
	put(t, filepath.Join(dir, ".git", "config"), "")
	if err := os.Symlink(filepath.Join(dir, "zdir"), filepath.Join(dir, "alink")); err != nil {
		t.Skip("no symbolic links here")
	}
	_ = os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "broken"))
	got, err := New().List(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []ide.Entry{{Name: "alink", Dir: true}, {Name: "zdir", Dir: true}, {Name: "a.go"}, {Name: "b.go"}, {Name: "broken"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d is %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, err := New().List(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("a missing directory was listed")
	}
}

func TestSourcesWalksEveryPackageAndSkipsWhatIsNotTheProjects(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"go.mod", "main.go", "internal/core/a.go", "internal/core/b_test.go", "docs/guide.md", "cmd/tool/main.go"} {
		put(t, filepath.Join(dir, name), "")
	}
	for _, name := range []string{".git/HEAD", "vendor/x/x.go", "node_modules/y/y.js", "bazel-bin/out.go", "logo.png", "internal/.cache/z.go", ".env"} {
		put(t, filepath.Join(dir, name), "")
	}
	_ = os.Symlink(dir, filepath.Join(dir, "internal", "loop"))
	got, err := New().Sources(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "cmd/tool/main.go docs/guide.md go.mod internal/core/a.go internal/core/b_test.go main.go"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v", got)
	}
}

func TestSourcesStopsAtItsLimitAndPassesOverWhatItCannotRead(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "ok.go"), "")
	put(t, filepath.Join(dir, "locked", "x.go"), "")
	if os.Getuid() != 0 {
		_ = os.Chmod(filepath.Join(dir, "locked"), 0)
		defer os.Chmod(filepath.Join(dir, "locked"), 0o755)
	}
	got, err := New().Sources(dir)
	if err != nil || len(got) < 1 || got[len(got)-1] != "ok.go" {
		t.Fatalf("got %v, %v", got, err)
	}
	if skipOnError(nil) != nil {
		t.Fatal("an unreadable file stopped the walk")
	}
}

func TestRootIsTheNearestDirectoryWithAGoMod(t *testing.T) {
	dir := t.TempDir()
	put(t, filepath.Join(dir, "go.mod"), "module x\n")
	deep := filepath.Join(dir, "a", "b")
	put(t, filepath.Join(deep, "x.go"), "")
	if got := Root(deep); got != dir {
		t.Fatalf("got %s", got)
	}
	bare := t.TempDir()
	if got := Root(bare); got != bare && !strings.HasPrefix(bare, got) {
		t.Fatalf("got %s", got)
	}
	space := t.TempDir()
	put(t, filepath.Join(space, "go.work"), "go 1.24\n")
	put(t, filepath.Join(space, "tools", "x.go"), "")
	if got := New().Project(filepath.Join(space, "tools")); got != space {
		t.Fatalf("a workspace is a project too: got %s", got)
	}
}

func TestEditableGoesByTheName(t *testing.T) {
	for name, want := range map[string]bool{"a.go": true, "BUILD": true, "x.PNG": false, ".env": false, "lib.so": false, "notes.md": true} {
		if editable(name) != want {
			t.Errorf("editable(%q) = %v", name, !want)
		}
	}
}

func BenchmarkTextOf(b *testing.B) {
	data := []byte(strings.Repeat("package main // żółw\n", 2000))
	for b.Loop() {
		_, _ = textOf(data)
	}
}

func BenchmarkEditable(b *testing.B) {
	for b.Loop() {
		editable("internal/core/thing.go")
	}
}
