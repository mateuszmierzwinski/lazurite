package lsp

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/mateuszmierzwinski/lazurite/internal/langkit/language"
)

// killGrace is how long a server has to leave after being asked, before it is
// killed. Cleanup that can block forever is worse than cleanup that is
// impolite: a window closing must not wait on a wedged subprocess.
const killGrace = 2 * time.Second

// NewLauncher builds the launcher that runs a server as a child process.
//
// Returns the INTERFACE (Rule 7.2), which is what lets every test in this
// package speak the protocol over a pair of pipes instead of starting a real
// language server — a test that needed gopls installed would fail on a machine
// without it, and this one is such a machine.
func NewLauncher() Launcher { return processLauncher{} }

// processLauncher starts a server with os/exec.
//
// # What is trusted
//
// The command is the USER's: it comes from lazurite's configuration or from their
// PATH, and it runs with their privileges. That is the trust boundary, and this
// launcher does not pretend to be a sandbox.
//
// What it does guarantee is that the command is EXECUTED, never interpreted.
// There is no shell anywhere on this path: a configured command of
// `gopls; rm -rf ~` is looked up as one executable with that improbable name
// and fails to be found, rather than becoming two commands.
type processLauncher struct{}

// Launch starts the server and hands back its stdio.
//
// The context is the server's LIFETIME. Cancelling it interrupts the process
// and, after killGrace, kills it — which is what makes a closing window leave
// no orphan behind.
func (processLauncher) Launch(ctx context.Context, spec language.ServerSpec) (Transport, error) {
	if spec.Command == "" {
		return nil, ErrNotAServerCommand
	}
	binary, err := exec.LookPath(spec.Command)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	run, cancel := context.WithCancel(ctx)
	cmd := command(run, binary, spec)
	transport, err := pipe(cmd, spec, cancel)
	if err != nil {
		cancel()
		return nil, err
	}
	return transport, nil
}

// command builds the child process, short of its pipes.
//
// Cancel INTERRUPTS rather than kills, and WaitDelay is what turns that into a
// kill for a server that ignores it. A straight kill would deny a well-behaved
// server the chance to clean up its own temporary files and caches.
func command(ctx context.Context, binary string, spec language.ServerSpec) *exec.Cmd {
	cmd := exec.CommandContext(ctx, binary, spec.Args...)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = killGrace
	cmd.Dir = spec.Root
	if len(spec.Env) > 0 {
		cmd.Env = spec.Env
	}
	return cmd
}

// pipe attaches the stdio and starts the process.
func pipe(cmd *exec.Cmd, spec language.ServerSpec, cancel context.CancelFunc) (Transport, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = newStderrLog(spec.Command)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &procTransport{cmd: cmd, stdin: stdin, stdout: stdout, cancel: cancel}, nil
}

// procTransport is a running server's stdio.
type procTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cancel context.CancelFunc

	closing sync.Once
	err     error
}

// Read is the server's stdout. EOF is not swallowed: it is how the reader
// goroutine learns the server has gone.
func (p *procTransport) Read(b []byte) (int, error) { return p.stdout.Read(b) }

// Write is the server's stdin.
func (p *procTransport) Write(b []byte) (int, error) { return p.stdin.Write(b) }

// Close ends the process and reaps it.
//
// In this order, and the order is the whole of it: stdin first, so a
// well-behaved server sees EOF and exits of its own accord; then the context,
// which interrupts and eventually kills one that does not; then stdout, which
// is what unblocks the reader goroutine; then Wait, which reaps the child and
// joins the stderr copier os/exec runs. Skipping the Wait leaves a zombie for
// every server the session ever restarted.
func (p *procTransport) Close() error {
	p.closing.Do(func() {
		_ = p.stdin.Close()
		p.cancel()
		_ = p.stdout.Close()
		p.err = p.cmd.Wait()
	})
	return p.err
}

// stderrLog is where a server's diagnostics go.
//
// Not discarded: a server that fails to start says why on stderr, and throwing
// that away makes "the language server is unavailable" a message the operator
// can do nothing with. Not passed through either: a chatty server would fill
// the journal, so the first few lines are kept and the rest is counted.
type stderrLog struct {
	name string
	mu   sync.Mutex // protects written; os/exec's copier is the only writer today
	// written bounds what reaches the log. A server logging every request at
	// debug level is a real thing, and it must cost the operator one line.
	written int
}

// newStderrLog builds the drain for one server's stderr.
func newStderrLog(name string) io.Writer { return &stderrLog{name: name} }

// maxStderrBytes is how much of a server's stderr is worth keeping. Enough for
// a stack trace and a configuration complaint; not enough to be a log file.
const maxStderrBytes = 8 << 10

// Write logs what the server said, up to the cap, and says once when it stops.
func (s *stderrLog) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.written >= maxStderrBytes {
		return len(p), nil
	}
	s.written += len(p)
	log.Printf("lazurite/lsp: %s: %s", s.name, p)
	if s.written >= maxStderrBytes {
		log.Printf("lazurite/lsp: %s: further output suppressed", s.name)
	}
	return len(p), nil
}
