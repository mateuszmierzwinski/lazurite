package language

import (
	"context"
	"errors"
	"testing"
)

// stubClient is a Client that records that it was stopped. It exists so the
// router's tests do not need the LSP adapter, which would invert the
// dependency this package is here to hold.
type stubClient struct {
	stopped bool
	status  Status
}

func (s *stubClient) Start(context.Context) error                            { return nil }
func (s *stubClient) Status() Status                                         { return s.status }
func (s *stubClient) Capabilities() Capabilities                             { return Capabilities{Hover: true} }
func (s *stubClient) Request(context.Context, string, any, any) error        { return nil }
func (s *stubClient) Notify(context.Context, string, any) error              { return nil }
func (s *stubClient) DidOpen(context.Context, Document) error                { return nil }
func (s *stubClient) DidChange(context.Context, string, int, []Change) error { return nil }
func (s *stubClient) DidSave(context.Context, string, string) error          { return nil }
func (s *stubClient) DidClose(context.Context, string) error                 { return nil }
func (s *stubClient) Stop(context.Context) error                             { s.stopped = true; return nil }

func TestAbsentSatisfiesTheWholePort(t *testing.T) {
	var client Client = Absent()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Absent().Start = %v, want nil", err)
	}
	if got := client.Status(); got != StatusStopped {
		t.Fatalf("Absent().Status = %v, want StatusStopped", got)
	}
	if client.Capabilities().Hover {
		t.Fatal("Absent() advertised a capability; the zero value must support nothing")
	}
	if err := client.Stop(context.Background()); err != nil {
		t.Fatalf("Absent().Stop = %v, want nil", err)
	}
}

func TestAbsentRefusesRequestsAndDiscardsNotifications(t *testing.T) {
	client := Absent()
	if err := client.Request(context.Background(), "textDocument/hover", nil, nil); !errors.Is(err, ErrNoServer) {
		t.Fatalf("Absent().Request = %v, want ErrNoServer", err)
	}
	// Document synchronisation is DISCARDED, not refused: an editor that had to
	// branch on whether a server exists before every keystroke is the branch
	// this null object removes.
	if err := client.DidOpen(context.Background(), Document{Path: "a.go"}); err != nil {
		t.Fatalf("Absent().DidOpen = %v, want nil", err)
	}
	if err := client.DidChange(context.Background(), "a.go", 2, FullChange("x")); err != nil {
		t.Fatalf("Absent().DidChange = %v, want nil", err)
	}
	if err := client.DidSave(context.Background(), "a.go", ""); err != nil {
		t.Fatalf("Absent().DidSave = %v, want nil", err)
	}
	if err := client.DidClose(context.Background(), "a.go"); err != nil {
		t.Fatalf("Absent().DidClose = %v, want nil", err)
	}
}

func TestServersRoutesByExtension(t *testing.T) {
	router := NewServers()
	gopls := &stubClient{status: StatusReady}
	router.Register(ServerSpec{Name: "gopls", Extensions: []string{"GO", ".mod"}}, gopls)
	if router.For("/w/main.go") != Client(gopls) {
		t.Fatal("a .go file did not route to the server claiming it")
	}
	if router.For("/w/go.mod") != Client(gopls) {
		t.Fatal("a .mod file did not route to the server claiming it")
	}
}

func TestServersAnswersAbsentForAnUnclaimedFile(t *testing.T) {
	router := NewServers()
	router.Register(ServerSpec{Extensions: []string{".go"}}, &stubClient{})
	client := router.For("/w/README.md")
	if client == nil {
		t.Fatal("For returned nil; a router must always answer with something callable")
	}
	if err := client.Request(context.Background(), "textDocument/hover", nil, nil); !errors.Is(err, ErrNoServer) {
		t.Fatalf("an unclaimed file answered %v, want ErrNoServer", err)
	}
	// An empty router is the ordinary state on a machine with nothing
	// installed, and it must behave exactly the same way.
	if err := NewServers().For("/w/main.go").Request(context.Background(), "x", nil, nil); !errors.Is(err, ErrNoServer) {
		t.Fatalf("an empty router answered %v, want ErrNoServer", err)
	}
}

func TestServersKeepsTheFirstClaimOnAnExtension(t *testing.T) {
	router := NewServers()
	first, second := &stubClient{}, &stubClient{}
	router.Register(ServerSpec{Extensions: []string{".go"}}, first)
	router.Register(ServerSpec{Extensions: []string{".go"}}, second)
	if router.For("/w/main.go") != Client(first) {
		t.Fatal("a second claim on .go displaced the first; the winner must be predictable")
	}
}

func TestServersIgnoresANilClientAndAnEmptyExtension(t *testing.T) {
	router := NewServers()
	router.Register(ServerSpec{Extensions: []string{".go"}}, nil)
	router.Register(ServerSpec{Extensions: []string{"", ".", "  "}}, &stubClient{})
	if err := router.For("/w/main.go").Request(context.Background(), "x", nil, nil); !errors.Is(err, ErrNoServer) {
		t.Fatal("a nil client was registered")
	}
	if err := router.For("/w/Makefile").Request(context.Background(), "x", nil, nil); !errors.Is(err, ErrNoServer) {
		t.Fatal("an empty extension became a routing key")
	}
}

func TestStopAllStopsEveryRegisteredServer(t *testing.T) {
	router := NewServers()
	one, two := &stubClient{}, &stubClient{}
	router.Register(ServerSpec{Extensions: []string{".go"}}, one)
	router.Register(ServerSpec{Extensions: []string{".rs"}}, two)
	router.StopAll(context.Background())
	if !one.stopped || !two.stopped {
		t.Fatalf("StopAll stopped %v and %v; both were registered", one.stopped, two.stopped)
	}
}

func TestSeverityAndStatusName(t *testing.T) {
	for value, want := range map[Severity]string{
		SeverityError: "error", SeverityWarning: "warning",
		SeverityInfo: "info", SeverityHint: "hint", Severity(99): "unknown",
	} {
		if got := value.String(); got != want {
			t.Fatalf("Severity(%d).String() = %q, want %q", value, got, want)
		}
	}
	for value, want := range map[Status]string{
		StatusStopped: "stopped", StatusStarting: "starting",
		StatusReady: "ready", StatusFailed: "failed",
	} {
		if got := value.String(); got != want {
			t.Fatalf("Status(%d).String() = %q, want %q", value, got, want)
		}
	}
}

func TestFullChangeIsAWholeDocumentReplacement(t *testing.T) {
	changes := FullChange("package main")
	if len(changes) != 1 || changes[0].Range != nil || changes[0].Text != "package main" {
		t.Fatalf("FullChange = %+v, want one change with no range", changes)
	}
}

// TestResetHandsBackTheOldServersAndFreesTheirExtensions — a server rooted at
// the project left behind must not keep claiming .go, or the new project's
// server is registered and never asked anything (FIRST claim wins).
func TestResetHandsBackTheOldServersAndFreesTheirExtensions(t *testing.T) {
	router := NewServers()
	old := &stubClient{status: StatusReady}
	router.Register(ServerSpec{Extensions: []string{".go"}}, old)
	gone := router.Reset()
	if len(gone) != 1 || gone[0] != Client(old) {
		t.Fatalf("Reset handed back %v", gone)
	}
	fresh := &stubClient{status: StatusReady}
	router.Register(ServerSpec{Extensions: []string{".go"}}, fresh)
	if router.For("/w/main.go") != Client(fresh) {
		t.Fatal("after a reset .go still routed to the old server")
	}
	if len(router.Reset()) != 1 {
		t.Fatal("the old server was still listed after it had been reset away")
	}
}
