package gowork

// Env is what the decision reads about the machine.
//
// Every field is a value the caller has already looked up, so this package
// never reads the environment itself and a test can describe any machine it
// likes — the same arrangement `toolpath` uses, for the same reason: where a
// file is is an adapter's business, and which answer follows from it is a rule.
type Env struct {
	// Root is the project folder lazurite has open, as an absolute path.
	Root string

	// GOWORK is the operator's own setting. A value they set themselves is
	// taken as given and nothing here overrides it: an operator who pointed the
	// toolchain at a particular workspace has answered this question already.
	GOWORK string
}

// Look is the two questions the decision asks the filesystem.
type Look struct {
	// ReadFile reads a file and reports whether it was there.
	ReadFile func(path string) ([]byte, bool)

	// Exists reports whether a path is present at all.
	Exists func(path string) bool
}
