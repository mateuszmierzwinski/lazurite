package ide

import (
	"sync"
	"testing"
	"time"
)

func TestWorkOffTheLoopIsDoneInTheOrderItWasGiven(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	w := newWorker(done)
	var mu sync.Mutex
	var order []int
	finished := make(chan struct{})
	for i := 0; i < 500; i++ {
		w.do(func() {
			if i%50 == 0 {
				time.Sleep(time.Millisecond)
			}
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
		})
	}
	w.do(func() { close(finished) })
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the work was never done")
	}
	for i, got := range order {
		if got != i {
			t.Fatalf("piece %d was done in place %d", got, i)
		}
	}
}

func TestTheIDEsOwnWorkerIsStartedOnDemandAndEndedByShutdown(t *testing.T) {
	r := start(t)
	r.app.deps.Spawn = nil
	ran := make(chan struct{})
	r.app.spawn(func() { close(ran) })
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the work was never done")
	}
	r.app.Shutdown()
	r.app.Shutdown()
	r.app.spawn(func() { t.Error("work was done after the IDE shut down") })
	time.Sleep(50 * time.Millisecond)
}
