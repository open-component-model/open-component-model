//go:build unix

package blob

import "os"

// detachSpoolFile unlinks the file while it stays open: its space is freed when the handle
// is closed, which the operating system also does when the process dies.
func detachSpoolFile(file *os.File) error {
	return os.Remove(file.Name())
}

func releaseSpoolFile(file *os.File) error {
	return file.Close()
}
