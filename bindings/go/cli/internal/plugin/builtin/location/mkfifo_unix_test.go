//go:build !windows

package location

import "golang.org/x/sys/unix"

func mkfifo(path string) error {
	return unix.Mkfifo(path, 0o666)
}
