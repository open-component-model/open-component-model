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
	if err := os.Chmod(dst, 0o644); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}
