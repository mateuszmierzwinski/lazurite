package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/mateuszmierzwinski/lazurite/internal/langkit/framing"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// The real process launcher is exercised against THIS TEST BINARY re-run as a
// trivial language server, never against gopls.
//
// A test that required a language server installed would fail on most machines,
// including the one this was written on. Re-running our own binary costs no
// dependency at all and still exercises the parts a pipe-based fake cannot:
// LookPath, the pipes, the interrupt, and the Wait that stops every restart
// leaving a zombie behind.

// fakeServerEnv puts the child process into language-server mode.
const fakeServerEnv = "LAZURITE_LSP_TEST_SERVER"

// init turns this binary into a language server when asked.
//
// In init rather than in a test, so it runs BEFORE the testing framework has
// printed a single byte. A framework banner on stdout is framing garbage in the
// middle of the protocol, and the client would be right to refuse it.
func init() {
	switch os.Getenv(fakeServerEnv) {
	case "":
		return
	case "crash":
		os.Exit(3)
	}
	serveAsLanguageServer()
	os.Exit(0)
}

// serveAsLanguageServer answers the handshake and leaves when told.
func serveAsLanguageServer() {
	stdin := bufio.NewReader(os.Stdin)
	for {
		body, err := framing.ReadFrame(stdin)
		if err != nil {
			return
		}
		var msg incoming
		if json.Unmarshal(body, &msg) != nil || len(msg.ID) == 0 {
			if msg.Method == methodExit {
				return
			}
			continue
		}
		answerAsLanguageServer(msg)
	}
}

// answerAsLanguageServer replies to one request.
func answerAsLanguageServer(msg incoming) {
	result := "null"
	if msg.Method == methodInitialize {
		result = `{"capabilities":{"textDocumentSync":1,"hoverProvider":true}}`
	}
	_ = framing.WriteFrame(os.Stdout, []byte(`{"jsonrpc":"2.0","id":`+string(msg.ID)+`,"result":`+result+`}`))
}

// selfSpec runs this binary as a language server.
func selfSpec(t *testing.T, mode string) language.ServerSpec {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		binary = os.Args[0]
	}
	if !filepath.IsAbs(binary) {
		t.Skipf("cannot locate this test binary to re-run (%q)", binary)
	}
	return language.ServerSpec{
		Name: "self", Command: binary, Root: t.TempDir(),
		Env: append(os.Environ(), fakeServerEnv+"="+mode),
	}
}

func TestTheProcessLauncherRunsAServerAndShutsItDown(t *testing.T) {
	spec := selfSpec(t, "serve")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := New(Deps{Spec: spec, Lifetime: ctx, MaxRestarts: -1, HandshakeTimeout: 10 * time.Second})
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start = %v", err)
	}
	if !client.Capabilities().Hover {
		t.Fatalf("capabilities = %+v; the handshake did not reach a real process", client.Capabilities())
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	if err := client.Stop(stopCtx); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	if client.Status() != language.StatusStopped {
		t.Fatalf("status = %v, want stopped", client.Status())
	}
}

func TestAServerThatDiesAtOnceLeavesTheEditorWithNoLanguageFeatures(t *testing.T) {
	spec := selfSpec(t, "crash")
	reporter := &recordingReporter{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := New(Deps{
		Spec: spec, Lifetime: ctx, Reporter: reporter, MaxRestarts: -1,
		HandshakeTimeout: 5 * time.Second,
	})
	if err := client.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded against a process that exits immediately")
	}
	if client.Status() != language.StatusFailed {
		t.Fatalf("status = %v, want failed", client.Status())
	}
	if !toldTheOperator(reporter, "no language features") {
		t.Fatalf("the operator was told %v", reporter.said())
	}
}

func TestTheLauncherRefusesASpecWithNoCommand(t *testing.T) {
	if _, err := NewLauncher().Launch(context.Background(), language.ServerSpec{}); !errors.Is(err, ErrNotAServerCommand) {
		t.Fatalf("Launch = %v, want ErrNotAServerCommand", err)
	}
}

func TestTheLauncherReportsAMissingBinaryRatherThanHanging(t *testing.T) {
	spec := language.ServerSpec{Command: "no-such-language-server-anywhere"}
	if _, err := NewLauncher().Launch(context.Background(), spec); err == nil {
		t.Fatal("Launch succeeded with nothing to launch")
	}
}

func TestTheLauncherNeverGoesThroughAShell(t *testing.T) {
	// A configured command carrying a shell separator is looked up as ONE
	// executable with an improbable name, and fails to be found. If this ever
	// starts succeeding, the launcher has grown a shell.
	spec := language.ServerSpec{Command: "echo hello; id"}
	if _, err := NewLauncher().Launch(context.Background(), spec); err == nil {
		t.Fatal("a command with a shell separator in it was launched")
	}
}

func TestStderrIsKeptButBounded(t *testing.T) {
	drain := newStderrLog("fakels")
	if _, err := drain.Write([]byte(strings.Repeat("x", maxStderrBytes))); err != nil {
		t.Fatalf("the stderr drain refused a write: %v", err)
	}
	kept := drain.(*stderrLog)
	if kept.written < maxStderrBytes {
		t.Fatalf("written = %d; the drain stopped counting", kept.written)
	}
	// Bounded, and still a Writer that accepts everything: refusing a write
	// would block the process whose stderr this is.
	if _, err := drain.Write([]byte("more")); err != nil {
		t.Fatalf("the drain refused a write after its cap: %v", err)
	}
}
