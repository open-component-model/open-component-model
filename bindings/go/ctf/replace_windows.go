package ctf

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// replaceFile renames src over dst. CTF files are written read-only, and
// Windows refuses to replace a read-only target, so the attribute is cleared
// on an existing dst first.
//
// Windows also refuses to replace a file that another process still has open,
// so a reader holding dst makes this fail with a sharing violation. dst survives
// that, so no data is lost; callers that replace a file concurrently with readers
// (SetIndex) have to retry.
func replaceFile(src, dst string) error {
	fi, err := os.Stat(dst)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return os.Rename(src, dst)
	case err != nil:
		return fmt.Errorf("unable to stat %q: %w", dst, err)
	}
	if err := os.Chmod(dst, 0o644); err != nil {
		return fmt.Errorf("unable to make %q writable: %w", dst, err)
	}
	if err := os.Rename(src, dst); err != nil {
		// dst survived the failed replace: leave it as read-only as it was found.
		if chmodErr := os.Chmod(dst, fi.Mode().Perm()); chmodErr != nil {
			err = errors.Join(err, fmt.Errorf("unable to restore the mode of %q: %w", dst, chmodErr))
		}
		return err
	}
	return nil
}
