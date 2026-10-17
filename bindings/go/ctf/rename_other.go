//go:build !windows

package ctf

import "os"

func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
