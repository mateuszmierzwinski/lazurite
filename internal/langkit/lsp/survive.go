package lsp

import (
	"fmt"
	"log"
	"runtime/debug"
)

// survive turns a panic on one of this package's goroutines into a message,
// instead of the operator's window closing under them.
//
// A panic on a goroutine takes the WHOLE PROCESS down. The goroutines here read
// a subprocess this editor did not write and hand its output to a consumer that
// did not write this package either, so a defect in either should cost the
// operator their language features and not the conversation and the unsaved
// buffer they were in the middle of.
//
// It is deliberately narrow, exactly as its sibling's is: it sits only where
// somebody else's code runs on our goroutine, it REPORTS rather than swallows,
// and the stack goes to stderr so the defect is still there to be found.
// Nothing on the frame path uses it — there is nothing of this package on the
// frame path — and a panic while painting is our own bug and should fail loudly.
func survive(what string, report func(string)) {
	fault := recover()
	if fault == nil {
		return
	}
	log.Printf("lazurite/lsp: %s panicked: %v\n%s", what, fault, debug.Stack())
	if report != nil {
		report(fmt.Sprintf("%s failed unexpectedly: %v", what, fault))
	}
}
