package filesystem

import (
	"crypto/rand"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// createSpoolFile creates the spool file in dir. Windows cannot remove the name of a file
// that is open, so the file is marked to be deleted when its handle is closed instead. The
// operating system closes the handle when the process dies, so the file does not outlive a
// killed process either. Until then it is visible in dir.
func createSpoolFile(dir string) (*os.File, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	// os.CreateTemp cannot pass the flag. The name is random enough to be unique, and
	// O_EXCL turns a collision into an error instead of opening someone else's file.
	name := filepath.Join(dir, spoolFilePrefix+rand.Text())
	return os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL|windows.O_FILE_FLAG_DELETE_ON_CLOSE, 0o600)
}
