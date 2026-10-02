# Lazurite

A console IDE for Go, in the spirit of the classic blue-screen IDEs: a menu
bar across the top, framed windows on a blue desktop, function keys that
build, run and debug — in any terminal, over SSH, with nothing to configure.

```
  File  Edit  Search  Run  Build  Debug  Window  Help
╔═══════ demo ═══════╗╔═[■]═════════════════ main.go ══════════════════1═══╗
║ ▾ shapes/          ║║package main                                        ║
║     shapes.go      ║║                                                    ║
║     shapes_test.go ║║import (                                            ║
║   go.mod           ║║    "fmt"                                           ║
║   main.go          ║║                                                    ║
║   README.md        ║║    "demo/shapes"                                   ║
║                    ║║)                                                   ║
║                    ║║                                                    ║
║                    ║║func main() {                                       ║
║                    ║║    total := 0                                      ║
║                    ║║    for i := 1; i <= 3; i++ {                       ║
║                    ║║        total += shapes.Square(i)                   ║
║                    ║║    }                                               ║
║                    ║║    fmt.Pr                                          ║
║                    ║║}         Print  func(a ...any) (n int, err error)  ║
║                    ║║          Printf  func(format string, a ...any)     ║
║                    ║║          Println  func(a ...any) (n int, err error)║
╚════════════════════╝╚═ 14:11 ════════════════════════════════════════════╝
╔══════════════════════════════ Debug ═══════════════════════════════════════╗
║ Variables                                                                  ║
║     total = 5  (int)                                                       ║
║     i = 3  (int)                                                           ║
╚═ Alt+M Messages · Alt+O Output · Alt+V Debug ══════════════════════════════╝
 F1 Help  F2 Save  F3 Open  F9 Build  Ctrl+F9 Run  F4 Debug  Alt+T Tree  F10 Menu
```

## About this repository

**Lazurite is exported from a private monorepo. No development happens on
GitHub.** This repository is a read-only mirror of the source; the code is
written, built, tested and released inside the monorepo and pushed here as a
snapshot, so it carries no development history.

- **Issues and feature requests are welcome** — they are read, and they do
  influence what gets built.
- **Pull requests cannot be accepted.** The branch here is regenerated
  wholesale on each export and a commit made against it would be
  overwritten. If you want a change, open an issue describing it.

Packages: an RPM for Fedora and a tarball for every other Linux are at
<https://mattmierzwinski.com/software/lazurite>.

## What it does

- **Edits many files at once**, across every package and directory of a
  module. The project is a tree at the left (`Alt+T`); `F3` browses
  directories, `Ctrl+P` lists every file of the project, `F6` and
  `Alt+1…9` switch between open files.
- **Starts a project for you.** *File → New Go project* makes the
  directory, has the Go tool write its `go.mod`, adds a `main.go`, a
  `README.md` and a `.gitignore`, and moves in — `Ctrl+F9` runs it at once.
  *File → Open project* moves to another one.
- **Knows when it is in a monorepo.**
  - In a **Bazel workspace** the same keys use Bazel: `F9` builds the
    project's targets, `Alt+F9` and `Shift+F9` build and test the package
    of the file you are in, `Ctrl+F9` runs the target you chose.
  - `Ctrl+E` finds a target by typing — a name, or a pattern such as
    `_test$` — across the whole workspace, the project's own first; Enter
    then offers Run, Test, Build, "use for Ctrl+F9" and "open its BUILD
    file".
  - A module that the **`go.work`** above it does not list is refused by the
    whole Go toolchain, and an editor in it has no completion at all.
    Lazurite sees that, runs its tools standalone (`GOWORK=off`) so
    everything works, says so in the tree's frame, and *File → Add project
    to go.work* lists it properly.
  - A new project made inside a Bazel workspace gets a `BUILD` file, and
    one made under a `go.work` asks whether to be added to it.
- **Completes as you type** — names, fields, methods, packages you have not
  imported yet, in a file you have not saved yet — and shows what is wrong
  while you write, through `gopls`. The status line says while `gopls` is
  still loading the project (a large workspace takes a while the first
  time) and when it is ready.
- **Cleans up on every save**: the file is formatted, the imports it uses
  are added and the ones it does not are removed. `Ctrl+K` does the same
  without saving. This part needs no extra tool: it is built in.
- **Builds and tests on a key.** `F9` builds the whole module, `Alt+F9` the
  package of the file you are in, `Shift+F9` runs its tests. A compiler
  error or a failing test is a line you can press Enter on; after a failed
  build the caret is already on the first one.
- **Runs your program on the real terminal.** `Ctrl+F9` steps aside, the
  program has the keyboard and the screen, and you come back when it ends.
  `Alt+F5` shows that screen again.
- **Debugs** with breakpoints (`Ctrl+F8`), step over / into / out
  (`F8` / `F7` / `Shift+F8`), variables you can open, watches, and
  expression evaluation — with the debugger built in. There is no `dlv` to
  install.
- **Goes to definitions** (`F12`) in other packages, the standard library
  and your dependencies, and describes the symbol under the caret
  (`Ctrl+D`).
- Is **one static binary** with no configuration file.

## Requirements

- Linux. Built and tested on x86-64; arm64 uses the same code and is
  untested.
- The Go toolchain on `PATH`, for building, testing, running and debugging.
- Optional: [`gopls`](https://pkg.go.dev/golang.org/x/tools/gopls) on
  `PATH` for completion, live problems and go-to-definition
  (`go install golang.org/x/tools/gopls@latest`). Without it Lazurite is
  still an editor that formats, imports, builds, tests, runs and debugs.

## Build and run

```sh
go build -o lazurite ./cmd/lazurite
./lazurite                 # the project in the current directory
./lazurite main.go         # open a file; the project is its module
./lazurite ~/src/myproject # open a project
```

The project is the nearest directory at or above where you start that has
a `go.mod` (or a `go.work`). Lazurite fills the terminal it is in, and still
does when whatever started it kept its output for itself: it then draws on
the terminal directly. Under `bazel run` it starts where you were, not where
Bazel put it.

| Option | Meaning |
|---|---|
| `-go PATH` | the Go tool to use (default `go`) |
| `-gopls PATH` | the language server; `-gopls ""` to do without |
| `-bazel PATH` | the Bazel to use inside a Bazel workspace (default `bazel`) |
| `-no-debugger` | start without the built-in debugger |
| `-log FILE` | write diagnostics to a file (they are discarded otherwise) |
| `-version` | print the version |

## Keys

| | |
|---|---|
| **Files** | `F3` open · `Ctrl+P` open in project · `F2` save · `Ctrl+N` new · `Alt+F3` close · `Alt+X` exit |
| **Windows** | `F6` / `Shift+F6` next / previous · `Alt+1…9` · `Alt+0` list · `F5` zoom |
| **Project tree** | `Alt+T` show and enter (again: hide) · `Enter` open file / fold directory · `←` `→` fold · `Esc` back to the text |
| **Bottom window** | `Alt+M` messages · `Alt+O` output · `Alt+V` debug · `Enter` go to · `Esc` back to the text |
| **Editing** | `Ctrl+Z` / `Ctrl+Y` undo / redo · `Ctrl+X` `Ctrl+C` `Ctrl+V` · `Shift`+arrows select · `Ctrl+A` all |
| **Code** | `Ctrl+Space` complete · `Ctrl+K` clean up and fix imports |
| **Search** | `Ctrl+F` find · `Ctrl+L` again · `Ctrl+G` go to line · `F12` definition · `Ctrl+D` describe · `Alt+F8` / `Alt+F7` next / previous problem |
| **Build** | `F9` build all · `Alt+F9` build package · `Shift+F9` test package · `Ctrl+F2` stop |
| **Monorepo** | `Ctrl+E` Bazel targets (type to search, `Enter` for what to do with one) |
| **Run** | `Ctrl+F9` run · `Alt+F5` user screen |
| **Debug** | `F4` debug / continue · `F8` over · `F7` into · `Shift+F8` out · `Ctrl+F8` breakpoint · `Ctrl+F4` evaluate · `Ctrl+F7` watch |
| **Menus** | `F10`, or `Alt` and the highlighted letter |

Some terminals and desktops keep a key for themselves (`Alt+F4`, `F11`,
`Ctrl+F`-keys). Every command is also in the menus, and the common ones
have a second key: `Ctrl+S` save, `Ctrl+O` open, `Ctrl+W` close, `Ctrl+Q`
exit, `Ctrl+R` run, `Ctrl+T` test, `Ctrl+B` breakpoint. The mouse works
where the terminal reports it: click to place the caret, drag to select,
wheel to scroll, click a menu.

Copy puts the text on Lazurite's own clipboard and offers it to the
terminal's (OSC 52), so it reaches your desktop's clipboard on terminals
that allow it. Pasting from the desktop is the terminal's own paste.

## What runs, and when

Lazurite runs nothing because you opened a file or a directory: there are
no modelines, no project settings that execute, no tasks. Three things do
start programs, and each is something you asked for:

- **`gopls`**, when it is installed, is started for the project you opened
  and loads it (which runs `go list`). Use `-gopls ""` to look at code you
  do not trust.
- **Saving a Go file** consults the Go tool to find the packages your
  imports mean.
- **Build, test, run and debug** run the Go tool — or, in a Bazel
  workspace, Bazel — on the project, and run the project's own code.
  `Ctrl+E` asks Bazel what the workspace holds (`bazel query`).

What a file, a compiler, the language server or a debugged program says is
never passed to your terminal raw: control characters are not drawn, and
the invisible characters that can make source read differently from what
the compiler sees (bidirectional overrides, zero-width characters) are
shown as `¿`.

## Limits worth knowing

- Linux only, for now: the terminal layer and the debugger are Linux's.
- A character wider than one cell (CJK, emoji) is kept in line but drawn
  over the cell after it.
- Breakpoints stay on their line number when lines are added above them.
- The debugger debugs the program (`package main`), not a package's tests.
- Rename, find-references and code actions are not offered yet.

## How it is made

Hexagonal: the IDE itself (`internal/core`) knows no terminal, disk,
compiler, language server or debugger — only ports. The adapters
(`internal/adapters`) are the terminal (raw mode and ANSI, standard library
only), the disk, the Go tool, the in-process clean-up, `gopls` over LSP, and
Delve linked in. The whole IDE runs in a test with fakes behind the ports,
driven by key events and read back as a screen; the black-box suites
(`test/e2e`, `test/cve`) run the real wiring over a real project, and the
real binary on a pseudo-terminal.

```sh
go test ./internal/...                      # unit tests, no tools needed
go build -o lazurite ./cmd/lazurite
LAZURITE_BIN=$PWD/lazurite go test ./test/...   # real Go tool, gopls, debugger
```

The name: lazurite is the deep-blue mineral of lapis lazuli. The screen is
blue.
