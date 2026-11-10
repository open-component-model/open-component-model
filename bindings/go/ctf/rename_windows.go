package ctf

import (
	"errors"
	"io/fs"
	"os"
)

// replaceFile renames src over dst. CTF files are written read-only, and
// Windows refuses to replace a read-only target, so the attribute is cleared
// on an existing dst first.
func replaceFile(src, dst string) error {
	fi, err := os.Stat(dst)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return os.Rename(src, dst)
	case err != nil:
		return err
	}
	if err := os.Chmod(dst, 0o644); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		// dst survived the failed replace: leave it as read-only as it was found.
		return errors.Join(err, os.Chmod(dst, fi.Mode().Perm()))
	}
	return nil
}
