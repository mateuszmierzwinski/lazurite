package ide

import "github.com/mateuszmierzwinski/lazurite/internal/core/edit"

// NoIntel is the language intelligence of a machine that has none: it
// knows nothing and answers nothing, and the editor works without it.
func NoIntel() Intel { return noIntel{} }

// noIntel answers every question with nothing.
type noIntel struct{}

// Listen has nobody to tell: nothing is ever found wrong.
func (noIntel) Listen(func(string, []Problem)) {
	// Nothing will ever be heard.
}

// Open, Change, Save and Close are news nobody is listening to.
func (noIntel) Open(string, string) {
	// Nobody to tell.
}

// Change is news nobody is listening to.
func (noIntel) Change(string, string) {
	// Nobody to tell.
}

// Save is news nobody is listening to.
func (noIntel) Save(string, string) {
	// Nobody to tell.
}

// Close is news nobody is listening to.
func (noIntel) Close(string) {
	// Nobody to tell.
}

// Complete offers nothing.
func (noIntel) Complete(_ string, _ edit.Pos, done func([]Suggestion)) { done(nil) }

// Define finds nothing.
func (noIntel) Define(_ string, _ edit.Pos, done func(Place, bool)) { done(Place{}, false) }

// Describe knows nothing.
func (noIntel) Describe(_ string, _ edit.Pos, done func(string)) { done("") }

// Stop has nothing to stop.
func (noIntel) Stop() {
	// Nothing was started.
}
