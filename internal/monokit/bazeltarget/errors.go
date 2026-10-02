package bazeltarget

import "errors"

// Parse and search failures. A malformed line is expected — bazel prints the
// odd notice on stdout — and is skipped by Parse rather than failing a listing.
var (
	ErrMalformedLine  = errors.New("not a bazel label_kind line")
	ErrMalformedLabel = errors.New("not a bazel label")
	ErrQueryTooLong   = errors.New("the search is too long")
)
