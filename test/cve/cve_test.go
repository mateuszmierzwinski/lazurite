// Package cve_test is the security regression suite: the attacks that have
// worked against this class of product — terminal editors and IDEs, the
// programs that sit between a terminal and untrusted files, compilers,
// language servers and debuggers — tried against the real IDE.
//
// Each test is written from the attacker's side and named for what must
// not happen. The classes, and the disclosures they come from, are listed
// by the names of the tests below.
package cve_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/core/term"
	"github.com/mateuszmierzwinski/lazurite/test/stack"
)

const patience = 120 * time.Second

// rune values of the characters an attacker hides things with; written as
// numbers so that this file holds none of them itself.
const (
	escape      = "\x1b"
	bell        = "\x07"
	rightToLeft = rune(0x202e)
	isolate     = rune(0x2066)
	zeroWidth   = rune(0x200b)
)

// hostile is text that would take a terminal over if it reached one raw:
// a title change, a clipboard read, a screen clear, a device query, the
// single-byte forms of the same.
var hostile = escape + "]0;pwned" + bell + escape + "]52;c;?" + bell + escape + "[2J" + escape + "[6n" + escape + "P+q" + escape + "\\" + string(rune(0x9b)) + "31m" + string(rune(0x9d)) + "0;x" + string(rune(0x9c))

// renderOnly matches what the IDE's own renderer writes and nothing else:
// cursor moves, colours, the cursor's visibility, a screen clear.
var renderOnly = regexp.MustCompile(`^(?:\x1b\[\?25[lh]|\x1b\[0m|\x1b\[2J|\x1b\[\d+;\d+H|\x1b\[0(?:;4)?;\d+;\d+m|[^\x00-\x1f\x7f\x{80}-\x{9f}])*$`)

// clean fails when anything but the renderer's own sequences and printable
// text was sent to the terminal, or when a cell of the screen holds a
// character that is not printable.
func clean(t *testing.T, s *stack.Stack) {
	t.Helper()
	s.Screen()
	for i, cell := range s.App.Screen().Cells {
		if cell.R < ' ' || cell.R == 0x7f || (cell.R >= 0x80 && cell.R < 0xa0) {
			t.Fatalf("cell %d of the screen holds the control character %U", i, cell.R)
		}
	}
	if written := s.Terminal.Written(); !renderOnly.Match(written) {
		t.Fatalf("something other than the renderer's own sequences reached the terminal: %q", firstForeign(written))
	}
}

// firstForeign is the part of the terminal's input around the first byte
// the renderer would not have written.
func firstForeign(written []byte) string {
	for end := len(written); end > 0; end-- {
		if renderOnly.Match(written[:end]) {
			return string(written[end:min(end+60, len(written))])
		}
	}
	return string(written[:min(60, len(written))])
}

func project() map[string]string {
	return map[string]string{
		"go.mod":  "module demo\n\ngo 1.24\n",
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n",
	}
}

// fakeGo is a stand-in for the Go tool that leaves a mark when it is run
// and prints what a test tells it to.
func fakeGo(t *testing.T, body string) (tool, mark string) {
	t.Helper()
	dir := t.TempDir()
	tool, mark = filepath.Join(dir, "go"), filepath.Join(dir, "was-run")
	script := "#!/bin/sh\necho \"$@\" >> " + mark + "\n" + body + "\n"
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return tool, mark
}

// --- Terminal escape injection (the class behind "a file that takes over
// your terminal when you look at it"). ---

func TestAFilesContentCannotSendTheTerminalACommand(t *testing.T) {
	files := project()
	files["evil.go"] = "package main\n// " + hostile + "\nvar x = \"" + hostile + "\"\n"
	s := stack.Start(t, files, stack.Options{}).Open("evil.go")
	s.Sees("package main", "pwned")
	clean(t, s)
}

func TestAFilesNameCannotSendTheTerminalACommand(t *testing.T) {
	files := project()
	name := "a" + escape + "]0;pwned" + bell + ".go"
	files[name] = "package main\n"
	s := stack.Start(t, files, stack.Options{})
	s.Press(term.KeyF3)
	clean(t, s)
	s.Type("pwned").Press(term.KeyEnter).Sees("package main")
	clean(t, s)
	s.Alt('0')
	clean(t, s)
	s.Press(term.KeyEsc).Ctrl('p')
	clean(t, s)
}

func TestACompilersOrAProgramsOutputCannotSendTheTerminalACommand(t *testing.T) {
	tool, _ := fakeGo(t, "printf 'main.go:3:1: "+strings.ReplaceAll(hostile, "'", "")+"\\n'; printf '\\033]0;again\\007\\n'; exit 1")
	s := stack.Start(t, project(), stack.Options{Go: tool}).Open("main.go")
	s.Press(term.KeyF9).Awaits("Build failed", patience).Sees("pwned", "again")
	clean(t, s)
	s.Alt('o').Press(term.KeyHome, term.KeyDown)
	clean(t, s)
}

// --- Source that reads differently from what the compiler sees ("Trojan
// Source", CVE-2021-42574, and the invisible-character variants). ---

func TestCharactersThatReorderOrHideSourceAreShown(t *testing.T) {
	files := project()
	files["trojan.go"] = "package main\n\nvar access = \"user" + string(rightToLeft) + string(isolate) + " // admin\"\nvar na" + string(zeroWidth) + "me = 1\n"
	s := stack.Start(t, files, stack.Options{}).Open("trojan.go")
	s.Sees("\"user¿¿ // admin\"", "na¿me")
	for _, cell := range s.App.Screen().Cells {
		if cell.R == rightToLeft || cell.R == isolate || cell.R == zeroWidth {
			t.Fatalf("the screen holds the invisible character %U", cell.R)
		}
	}
	s.GoTo("4:8").Type("x").Press(term.KeyF2)
	if got := s.File("trojan.go"); !strings.Contains(got, string(rightToLeft)) || !strings.Contains(got, "na"+string(zeroWidth)+"xme") {
		t.Fatalf("showing the characters changed the file: %q", got)
	}
}

// --- Code execution by opening (Vim's modelines, CVE-2019-12735 and
// CVE-2026-34982; Emacs local variables; the "open a repository, get
// owned" class of workspace-trust bypasses in IDEs). ---

func TestOpeningAProjectOrAFileRunsNothing(t *testing.T) {
	tool, mark := fakeGo(t, "exit 0")
	files := project()
	files["notes.txt"] = "vim: set foldexpr=system('touch /tmp/owned'):\n-*- eval: (shell-command \"touch /tmp/owned\") -*-\n"
	files["gen.go"] = "package main\n\n//go:generate sh -c \"touch generated\"\n// vim: set modelineexpr:\n"
	files[".vscode/tasks.json"] = `{"version":"2.0.0","tasks":[{"label":"x","type":"shell","command":"touch tasked","runOptions":{"runOn":"folderOpen"}}]}`
	files[".lazurite"] = "run: touch configured\n"
	files[".envrc"] = "touch envrc\n"
	s := stack.Start(t, files, stack.Options{Go: tool}).Open("notes.txt").Open("gen.go").Open("main.go")
	s.Type("x").Ctrl('z').Press(term.KeyF3, term.KeyEsc).Ctrl('p').Press(term.KeyEsc).Alt('m').Press(term.KeyEsc)
	s.Open("notes.txt").Type("edited").Press(term.KeyF2).Sees("Saved notes.txt")
	if _, err := os.Stat(mark); err == nil {
		ran, _ := os.ReadFile(mark)
		t.Fatalf("opening and editing ran the Go tool: %s", ran)
	}
	for _, made := range []string{"generated", "tasked", "configured", "envrc"} {
		if _, err := os.Stat(s.Path(made)); err == nil {
			t.Fatalf("opening the project ran something that made %q", made)
		}
	}
}

// --- Command injection (arguments and names reaching a shell). ---

func TestArgumentsAndNamesAreNeverReadByAShell(t *testing.T) {
	tool, mark := fakeGo(t, `for a in "$@"; do echo "arg=[$a]"; done`)
	s := stack.Start(t, project(), stack.Options{Go: tool}).Open("main.go")
	planted := filepath.Join(s.Dir, "injected")
	s.App.Do("arguments")
	s.Type("; touch " + planted + " $(touch " + planted + ") `touch " + planted + "` | tee " + planted).Press(term.KeyEnter)
	s.Chord(term.KeyF9, term.Ctrl)
	if _, err := os.Stat(planted); err == nil {
		t.Fatal("an argument of the program was run as a command")
	}
	if out := s.Out.String(); !strings.Contains(out, "arg=[;]") || !strings.Contains(out, "arg=[$(touch]") || !strings.Contains(out, "arg=[|]") {
		t.Fatalf("the arguments did not reach the program as they were typed:\n%s", out)
	}
	ran, _ := os.ReadFile(mark)
	if !strings.HasPrefix(string(ran), "run . -- ; touch ") {
		t.Fatalf("the Go tool was run as: %s", ran)
	}
}

// --- Bracketed-paste injection (pasted text acted on as keys). ---

func TestPastedTextIsNeverActedOnAsKeys(t *testing.T) {
	s := stack.Start(t, project(), stack.Options{}).Open("main.go")
	before := s.File("main.go")
	s.Raw("\x1b[200~x\x1b[21~\x1bx\x13\x11\x1b[24~\x1b[201~")
	if s.App.Quitting() {
		t.Fatal("a pasted Alt+X or Ctrl+Q quit the IDE")
	}
	if s.File("main.go") != before {
		t.Fatal("a pasted Ctrl+S saved the file")
	}
	s.Sees(" main.go * ").Lacks("Exit", "Save as")
	clean(t, s)
	s.Raw("\x1b[200~" + strings.Repeat("y", term.MaxPaste+4096))
	s.Sees(" main.go * ")
}

// --- Insecure temporary and backup files, and writes through links
// (the swap-file, backup-file and symlink races of editors). ---

func TestSavingLeavesNoOtherFileAndKeepsAPrivateFilePrivate(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{})
	secret := s.Path("secret.go")
	if err := os.WriteFile(secret, []byte("package main\n\nvar key = \"hunter2\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.Open("secret.go").GoTo("3").Press(term.KeyEnd, term.KeyEnter).Type("var more = 1").Press(term.KeyF2).Sees("Saved secret.go")
	info, err := os.Stat(secret)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("a private file was saved with mode %v", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(s.Dir)
	for _, e := range entries {
		if name := e.Name(); name != "go.mod" && name != "main.go" && name != "secret.go" {
			t.Fatalf("saving left %q behind", name)
		}
	}
}

func TestSavingThroughALinkWritesWhereItPointsAndNowhereElse(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{})
	outside := filepath.Join(t.TempDir(), "real.txt")
	stack.Put(t, outside, "real\n")
	if err := os.Symlink(outside, s.Path("link.txt")); err != nil {
		t.Skip("no symbolic links here")
	}
	s.Open("link.txt").Type("edited ").Press(term.KeyF2)
	if got, _ := os.ReadFile(outside); string(got) != "edited real\n" {
		t.Fatalf("the file behind the link holds %q", got)
	}
	if info, err := os.Lstat(s.Path("link.txt")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced by a file")
	}
}

// --- Denial of service: things that never end, or are too big. ---

func TestWhatCannotBeATextIsRefusedAndTheIDEGoesOn(t *testing.T) {
	s := stack.Start(t, project(), stack.Options{})
	stack.Put(t, s.Path("blob.bin"), "ELF\x00\x01\x02\x03")
	big, err := os.Create(s.Path("huge.go"))
	if err != nil {
		t.Fatal(err)
	}
	_ = big.Truncate(64 << 20)
	big.Close()
	for _, name := range []string{"blob.bin", "huge.go", "."} {
		if err := s.App.Open(s.Path(name)); err == nil {
			t.Fatalf("%s was opened as a text", name)
		}
	}
	done := make(chan error, 1)
	go func() { done <- s.App.Open("/dev/zero") }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a device was opened as a text")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("opening a device never returned")
	}
	s.Open("main.go").Sees("package main")
}

func TestAnEnormousLineAndAnEnormousTerminalAreDrawnInBoundedTime(t *testing.T) {
	files := project()
	files["line.go"] = "package main\n\nvar s = \"" + strings.Repeat("x", 1<<20) + "\"\n"
	s := stack.Start(t, files, stack.Options{})
	began := time.Now()
	s.Open("line.go").GoTo("3").Press(term.KeyEnd).Type("y").Press(term.KeyHome)
	s.Send(term.Event{Kind: term.EvResize, X: 65535, Y: 65535})
	s.Screen()
	if w, h := s.App.Screen().W, s.App.Screen().H; w > term.MaxWidth || h > term.MaxHeight {
		t.Fatalf("a terminal claiming to be enormous was believed: %dx%d", w, h)
	}
	s.Send(term.Event{Kind: term.EvResize, X: 0, Y: -7}).Screen()
	if took := time.Since(began); took > 20*time.Second {
		t.Fatalf("took %v", took)
	}
}

func TestAFloodOfOutputIsBoundedAndCanBeStopped(t *testing.T) {
	tool, _ := fakeGo(t, "yes 'main.go:1:1: flood' | head -n 200000; head -c 3000000 /dev/zero | tr '\\0' z; echo; exec sleep 600")
	s := stack.Start(t, project(), stack.Options{Go: tool}).Open("main.go")
	s.Press(term.KeyF9)
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) && !strings.Contains(s.Screen(), "zzzz") {
		time.Sleep(10 * time.Millisecond)
	}
	s.Sees("zzzz")
	s.Chord(term.KeyF2, term.Ctrl).Awaits("Build failed", patience)
	clean(t, s)
}

func TestMalformedTerminalInputNeverStallsOrCrashesTheIDE(t *testing.T) {
	s := stack.Start(t, project(), stack.Options{}).Open("main.go")
	for _, input := range []string{
		"\x1b[" + strings.Repeat("9", 100000) + "~",
		"\x1b[<", "\x1b[<0;", "\x1b[<999999999999;-1;-1M", "\x1b[<0;0;0M", "\x1b[<0;70000;70000M",
		"\x1b[;;;;;;;;;~", "\x1bO", "\x1b[[", "\x1b\x1b\x1b\x1b", "\xff\xfe\xfd", "\x00\x00\x00", "\xc3", "\xf0\x9f",
		strings.Repeat("\x1b[A", 5000), "\x1b]52;c;?\x07", "\x1bP+q544e\x1b\\",
	} {
		s.Raw(input)
	}
	s.Press(term.KeyEsc, term.KeyEsc).Sees(" main.go ")
	if s.App.Quitting() {
		t.Fatal("rubbish from the terminal quit the IDE")
	}
	clean(t, s)
}

// --- The clipboard: the IDE offers text to it and never asks for it. ---

func TestTheClipboardIsOnlyEverWrittenAndTheTextCannotBreakOutOfTheSequence(t *testing.T) {
	files := project()
	files["clip.txt"] = "before" + bell + escape + "]0;title" + bell + "after\n"
	s := stack.Start(t, files, stack.Options{}).Open("clip.txt")
	s.Ctrl('a').Ctrl('c')
	written := string(s.Terminal.Written())
	at := strings.Index(written, escape+"]52;c;")
	if at < 0 {
		t.Fatal("the copy was not offered to the terminal")
	}
	sequence := written[at:]
	end := strings.Index(sequence, bell)
	if end < 0 || strings.Contains(sequence[end+1:], bell) || strings.Count(sequence, escape+"]") != 1 {
		t.Fatalf("the copied text broke out of the clipboard sequence: %q", sequence)
	}
	if strings.Contains(written, "]52;c;?") || strings.Contains(written, "]52;p;?") {
		t.Fatal("the IDE asked the terminal for the clipboard")
	}
}

// --- The debugger and the language server: in this process, over pipes,
// never a network listener (the class of "debug port open to the
// network": Delve's headless server, gopls -listen, CVE-2026-42503). ---

func TestDebuggingOpensNoNetworkListener(t *testing.T) {
	stack.NeedsGo(t)
	s := stack.Start(t, project(), stack.Options{Gopls: optionalGopls(t)}).Open("main.go")
	s.GoTo("6").Chord(term.KeyF8, term.Ctrl).Press(term.KeyF4)
	if !s.Shows("Call stack", patience) {
		t.Skipf("this machine will not let a program be debugged:\n%s", s.Screen())
	}
	for _, socket := range sockets(t) {
		t.Errorf("the IDE holds a socket while debugging: %s", socket)
	}
	s.Press(term.KeyF4).Awaits("Program ended", patience)
}

// optionalGopls is the language server when the machine has one.
func optionalGopls(t *testing.T) string {
	for _, dir := range append(filepath.SplitList(os.Getenv("PATH")), filepath.Join(os.Getenv("HOME"), ".local", "bin")) {
		if info, err := os.Stat(filepath.Join(dir, "gopls")); err == nil && info.Mode().IsRegular() {
			return filepath.Join(dir, "gopls")
		}
	}
	return ""
}

// sockets is the sockets this process holds open.
func sockets(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("no /proc here")
	}
	var out []string
	for _, e := range entries {
		if target, err := os.Readlink(filepath.Join("/proc/self/fd", e.Name())); err == nil && strings.HasPrefix(target, "socket:") {
			out = append(out, e.Name()+" → "+target)
		}
	}
	return out
}

// --- A new project: its name and its module are data, never commands, and
// it is never made over somebody's files. ---

func TestANewProjectsNameAndModuleAreNeverReadByAShellAndNothingIsOverwritten(t *testing.T) {
	tool, mark := fakeGo(t, "exit 1")
	s := stack.Start(t, project(), stack.Options{Go: tool}).Open("main.go")
	planted := filepath.Join(s.Dir, "injected")
	before := s.File("main.go")
	s.App.Do("project.new")
	s.Ctrl('a')
	for i := 0; i < 200; i++ {
		s.Press(term.KeyBackspace)
	}
	s.Type(s.Dir).Press(term.KeyEnter).Type("x").Press(term.KeyEnter)
	s.Sees("already holds files")
	if s.File("main.go") != before {
		t.Fatal("a new project was made over an existing one")
	}
	fresh := filepath.Join(filepath.Dir(s.Dir), "fresh $(touch "+planted+")")
	s.App.Do("project.new")
	for i := 0; i < 200; i++ {
		s.Press(term.KeyBackspace)
	}
	s.Type(fresh).Press(term.KeyEnter)
	for i := 0; i < 200; i++ {
		s.Press(term.KeyBackspace)
	}
	s.Type("m; touch " + planted + " `touch " + planted + "`").Press(term.KeyEnter)
	s.Awaits("Hello from injected)", patience)
	if _, err := os.Stat(planted); err == nil {
		t.Fatal("a project's name or module was run as a command")
	}
	ran, _ := os.ReadFile(mark)
	if !strings.Contains(string(ran), "mod init m; touch ") {
		t.Fatalf("the Go tool was run as: %s", ran)
	}
	if info, err := os.Stat(filepath.Join(fresh, "main.go")); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("the project's files were not made as ordinary files: %v", err)
	}
	clean(t, s)
}

// --- A workspace's BUILD files name its targets, and a BUILD file can come
// from anywhere: a target's name is data. ---

func TestAHostileTargetNameIsNeitherRunAsACommandNorSentToTheTerminal(t *testing.T) {
	dir := t.TempDir()
	planted := filepath.Join(dir, "planted")
	bazel := filepath.Join(dir, "bazel")
	script := "#!/bin/sh\nif [ \"$1\" = query ]; then\n" +
		"  printf 'go_binary rule //x:$(touch${IFS}" + planted + ")\\n'\n" +
		"  printf 'go_binary rule //x:a;touch${IFS}" + planted + "\\n'\n" +
		"  printf 'go_binary rule //x:\\033]0;pwned\\007\\033[2Jb\\n'\n  exit 0\nfi\n" +
		"for a in \"$@\"; do echo \"arg=[$a]\"; done\n"
	if err := os.WriteFile(bazel, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"MODULE.bazel": "", "BUILD": "", "go.mod": "module ws\n\ngo 1.24\n", "main.go": "package main\n\nfunc main() {}\n"}
	s := stack.Start(t, files, stack.Options{Bazel: bazel}).Open("main.go")
	s.Ctrl('e').Awaits("Bazel targets", patience)
	clean(t, s)
	s.Type("touch").Press(term.KeyEnter)
	clean(t, s)
	s.Press(term.KeyEnter)
	if _, err := os.Stat(planted); err == nil {
		t.Fatal("a target's name was run as a command")
	}
	if out := s.Out.String(); !strings.Contains(out, "arg=[run]") || !strings.Contains(out, "arg=[//x:$(touch${IFS}"+planted+")]") {
		t.Fatalf("the label did not reach Bazel as one argument:\n%s", out)
	}
	s.Ctrl('e').Type("pwned").Press(term.KeyEnter).Type("build").Press(term.KeyEnter).Awaits("Build succeeded", patience)
	clean(t, s)
}
