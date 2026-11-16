package blob

import (
	"errors"
	"os"
)

// detachSpoolFile does nothing: Windows cannot remove a file that is open.
func detachSpoolFile(*os.File) error {
	return nil
}

func releaseSpoolFile(file *os.File) error {
	return errors.Join(file.Close(), os.Remove(file.Name()))
}

// reopenSpoolFile is not available: the spool file is read through the handle it was
// created with.
func reopenSpoolFile(*os.File) (*os.File, error) {
	return nil, errors.ErrUnsupported
}
