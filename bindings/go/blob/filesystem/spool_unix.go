//go:build unix

package filesystem

import (
	"errors"
	"os"
)

// createSpoolFile creates the spool file in dir and unlinks it while it stays open. Its
// space is freed when the last handle to it is closed, which the operating system also does
// when the process dies. The file does not show up in dir afterwards.
func createSpoolFile(dir string) (*os.File, error) {
	file, err := os.CreateTemp(dir, spoolFilePrefix+"*")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(file.Name()); err != nil {
		err = errors.Join(err, file.Close())
		// Some mounts refuse to unlink a file only while it is open: try once more after
		// closing so no empty file is left behind. The first error already says why.
		_ = os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}
