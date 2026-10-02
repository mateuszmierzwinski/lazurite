package osfiles

import "errors"

// What is refused.
var (
	// ErrTooLarge is a file larger than MaxFile.
	ErrTooLarge = errors.New("the file is too large to edit")
	// ErrBinary is a file that is not text.
	ErrBinary = errors.New("the file is not text")
	// ErrNotAFile is a directory, a device, a pipe.
	ErrNotAFile = errors.New("not a regular file")
)
