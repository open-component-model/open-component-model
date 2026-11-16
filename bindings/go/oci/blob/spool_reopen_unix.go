//go:build unix && !linux

package blob

import (
	"errors"
	"os"
)

// reopenSpoolFile is not available: an unlinked file cannot be opened again here. On the
// BSDs and macOS, opening /dev/fd/N duplicates the handle, which shares its read position.
func reopenSpoolFile(*os.File) (*os.File, error) {
	return nil, errors.ErrUnsupported
}
