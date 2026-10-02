package ide

import "sync"

// worker does work off the loop, one piece at a time, in the order it was
// given.
//
// The order is the point. The debugger is asked things that depend on each
// other — remove this breakpoint, then let the program go — and asked on
// goroutines of their own, the second could arrive first: the program
// would stop at a breakpoint the user had just removed.
//
// mu protects queue. The queue has no bound of its own: what is put in it
// is put by keys pressed, and each piece is done in the time the debugger
// takes to answer.
type worker struct {
	mu    sync.Mutex
	queue []func()
	// wake holds at most one notice that there is work: more than one
	// would say nothing more.
	wake chan struct{}
}

// newWorker makes a worker and starts its goroutine, which ends when done
// is closed.
func newWorker(done <-chan struct{}) *worker {
	w := &worker{wake: make(chan struct{}, 1)}
	go w.run(done)
	return w
}

// do gives the worker a piece of work and returns at once.
func (w *worker) do(work func()) {
	w.mu.Lock()
	w.queue = append(w.queue, work)
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// next takes the oldest piece of work, when there is one.
func (w *worker) next() (func(), bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.queue) == 0 {
		return nil, false
	}
	work := w.queue[0]
	w.queue = w.queue[1:]
	return work, true
}

// closed reports whether a channel has been closed.
func closed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// run does the work as it comes, until done is closed. Work still waiting
// then is dropped.
func (w *worker) run(done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case <-w.wake:
		}
		for work, ok := w.next(); ok && !closed(done); work, ok = w.next() {
			work()
		}
	}
}
