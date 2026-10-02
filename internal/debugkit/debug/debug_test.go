package debug

import "testing"

// TestASessionNobodyStartedIsNotDebugging — Off is the zero value, so a window
// that has never debugged must not draw its step controls enabled.
func TestASessionNobodyStartedIsNotDebugging(t *testing.T) {
	var s Session
	if s.Debugging() || s.AtRest() {
		t.Fatalf("a zero session reports Debugging=%v AtRest=%v", s.Debugging(), s.AtRest())
	}
}

// TestOnlyAStoppedSessionCanBeInspected.
//
// Variables read while the debuggee runs describe a moment that has already
// passed, so AtRest gates every inspection and is deliberately narrower than
// Debugging.
func TestOnlyAStoppedSessionCanBeInspected(t *testing.T) {
	for state, wantRest := range map[State]bool{
		Off: false, Starting: false, Running: false, Stopped: true, Ended: false,
	} {
		if got := (Session{State: state}).AtRest(); got != wantRest {
			t.Errorf("state %d reports AtRest=%v, want %v", state, got, wantRest)
		}
	}
}

// TestARunningOrStoppedSessionIsDebugging — the controls are enabled for both,
// because a Stop is as much an action on a live session as a Continue.
func TestARunningOrStoppedSessionIsDebugging(t *testing.T) {
	for state, want := range map[State]bool{
		Off: false, Starting: false, Running: true, Stopped: true, Ended: false,
	} {
		if got := (Session{State: state}).Debugging(); got != want {
			t.Errorf("state %d reports Debugging=%v, want %v", state, got, want)
		}
	}
}

func BenchmarkDebugging(b *testing.B) {
	s := Session{State: Stopped}
	for b.Loop() {
		_ = s.Debugging()
	}
}
