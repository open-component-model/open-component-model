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
