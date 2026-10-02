# Changelog

All notable changes to Lazurite are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the same entries are
in the `%changelog` of `lazurite.spec`, which is what `rpm -q --changelog` and
`dnf` show.

## [0.1.0] — 2026-10-02

First release.

### Added

- A console IDE for Go with a classic text-mode desktop: menu bar, framed
  windows, dialogs, function keys. One static binary, no configuration file.
- Editing of many files across every package and directory of a module, with
  a project tree (`Alt+T`), a directory browser (`F3`), open-in-project
  (`Ctrl+P`), undo/redo, selection, find and replace, syntax colouring and
  the mouse.
- Completion as you type, live problems, go to definition (`F12`) and symbol
  description (`Ctrl+D`) through `gopls`, when it is installed. Completion
  works in a file that has not been saved yet.
- Clean-up on every save and on `Ctrl+K`: the file is formatted and its
  imports are added and removed to match what it uses. Built in — no tool to
  install.
- Build (`F9`, `Alt+F9`), test (`Shift+F9`) and run (`Ctrl+F9`) on keys.
  Errors and failing tests are lines you can press Enter on.
- A built-in debugger: breakpoints, step over / into / out, variables,
  watches and expression evaluation. There is no `dlv` to install.
- File → New Go project, which makes the directory, `go.mod`, `main.go`,
  `README.md` and `.gitignore`, and File → Open project.
- Monorepo handling: inside a Bazel workspace the build, test and run keys use
  Bazel, and `Ctrl+E` finds a target by typing. A module that the `go.work`
  above it does not list is built standalone so that completion and builds
  work, and File → Add project to go.work lists it.
- Two ways to get it: an RPM for Fedora, and a portable tarball for every
  other Linux — the same static binary, with an `install.sh` that puts it
  under `~/.local` (or `/usr/local` as root).

### Security

- Nothing a file, a file name, a compiler, the language server or a debugged
  program says is passed to the terminal raw: control characters are never
  drawn, so none of them can retitle the window, read the clipboard or
  repaint the screen.
- Invisible characters that can make source code read differently from what
  the compiler sees (bidirectional overrides, zero-width characters) are
  shown as `¿`.
- Opening a file or a project runs nothing: there are no modelines and no
  project settings that execute. Programs are started only by `gopls` loading
  the project, by saving a Go file (which asks the Go tool where imports
  live), and by the build, test, run and debug keys.
- Every command is run with its arguments as arguments, never through a
  shell, so a file, target or module name cannot be run as a command.
- The debugger and the language server are in this process or on a pipe;
  nothing listens on the network.

### What you need

- Linux, and the Go toolchain on `PATH`.
- Optional: `gopls` on `PATH` for completion and live problems
  (`go install golang.org/x/tools/gopls@latest`).
