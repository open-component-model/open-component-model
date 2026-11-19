//go:build unix

package ctf

import "os"

// replaceFile renames src over dst, replacing dst if it exists.
func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
