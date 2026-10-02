// Package osfiles is the disk: reading and writing the files being edited,
// listing directories, and finding a project's sources and its root.
package osfiles

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mateuszmierzwinski/lazurite/internal/core/ide"
)

// The limits of what is read from the disk.
const (
	// MaxFile is the largest file opened: an editor that holds a text in
	// memory, coloured, with undo, is not the tool for a gigabyte.
	MaxFile = 16 << 20
	// MaxSources is how many files a project's listing holds.
	MaxSources = 20000
	// MaxEntries is how many names of one directory are listed.
	MaxEntries = 5000
)

// Disk is the files of this machine.
type Disk struct{}

// New is the disk as the IDE's port.
func New() ide.Files { return Disk{} }

// Read is a file's text. Only a regular file is read — a pipe or a device
// would block or never end — and only one that is text and not too large.
func (Disk) Read(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", ErrNotAFile
	}
	if info.Size() > MaxFile {
		return "", ErrTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFile+1))
	if err != nil {
		return "", err
	}
	return textOf(data)
}

// textOf is bytes as text, refused when they are too many or not text.
func textOf(data []byte) (string, error) {
	if len(data) > MaxFile {
		return "", ErrTooLarge
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", ErrBinary
	}
	return string(data), nil
}

// newFileMode is the permissions of a file the editor makes, and
// newDirMode of a directory it makes for one.
const (
	newFileMode = 0o644
	newDirMode  = 0o755
)

// Write puts a text in a file, whole or not at all: it is written beside
// the file and moved over it, so a crash or a full disk leaves the old
// text rather than half of the new. A file reached through a link is
// written where the link points, and keeps its permissions. The
// directories above a new file are made when they are missing.
func (Disk) Write(path, text string) error {
	target, mode := resolve(path)
	if err := os.MkdirAll(filepath.Dir(target), newDirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".lazurite-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// resolve is where a path's file really is, and the permissions it has or
// should be made with.
func resolve(path string) (string, fs.FileMode) {
	target := path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		target = real
	}
	if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
		return target, info.Mode().Perm()
	}
	return target, newFileMode
}

// List is a directory's entries: directories first, each group by name.
// Names that start with a dot are left out.
func (Disk) List(dir string) ([]ide.Entry, error) {
	found, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []ide.Entry
	for _, e := range found {
		if strings.HasPrefix(e.Name(), ".") || len(out) >= MaxEntries {
			continue
		}
		out = append(out, ide.Entry{Name: e.Name(), Dir: isDir(dir, e)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Dir && !out[j].Dir })
	return out, nil
}

// isDir reports whether an entry is a directory, following a link to one.
func isDir(dir string, e fs.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&fs.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, e.Name()))
	return err == nil && info.IsDir()
}

// skipped is the directories a project's listing does not go into.
var skipped = map[string]bool{"vendor": true, "node_modules": true}

// Sources is every file under a root that could be edited, as paths
// relative to it. Hidden directories and vendored code are not entered,
// links are not followed, and the listing stops at MaxSources.
func (Disk) Sources(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return skipOnError(e)
		}
		name := e.Name()
		if e.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || skipped[name] || strings.HasPrefix(name, "bazel-")) {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, relErr := filepath.Rel(root, path); relErr == nil && e.Type().IsRegular() && editable(name) {
			out = append(out, rel)
		}
		if len(out) >= MaxSources {
			return filepath.SkipAll
		}
		return nil
	})
	return out, err
}

// skipOnError passes over what cannot be read: a directory is skipped, a
// file ignored.
func skipOnError(e fs.DirEntry) error {
	if e != nil && e.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

// binaries is the file endings that are not text.
var binaries = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true, ".pdf": true, ".zip": true,
	".gz": true, ".tar": true, ".exe": true, ".so": true, ".a": true, ".o": true, ".bin": true,
	".woff": true, ".woff2": true, ".ttf": true, ".wav": true, ".mp3": true, ".mp4": true, ".db": true,
}

// editable reports whether a file's name says it is text.
func editable(name string) bool {
	return !strings.HasPrefix(name, ".") && !binaries[strings.ToLower(filepath.Ext(name))]
}

// projectFiles is the files that make a directory a project.
var projectFiles = []string{"go.mod", "go.work"}

// Root is the project a directory belongs to: the nearest directory above
// it (or itself) that holds a go.mod or a go.work, or the directory itself
// when none does.
func Root(dir string) string {
	for at := dir; ; at = filepath.Dir(at) {
		if marksProject(at) {
			return at
		}
		if at == filepath.Dir(at) {
			return dir
		}
	}
}

// marksProject reports whether a directory holds a file that makes it a
// project.
func marksProject(dir string) bool {
	for _, name := range projectFiles {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// Project is Root, as the IDE's port asks for it.
func (Disk) Project(dir string) string { return Root(dir) }
