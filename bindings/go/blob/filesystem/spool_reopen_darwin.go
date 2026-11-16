package filesystem

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// reopenSpoolFile returns another handle to the spool file with its own read position.
// macOS cannot open an unlinked file again: opening /dev/fd/N duplicates the handle, which
// shares its position. Instead the file is cloned to a new name that is opened and removed
// at once. A clone shares the data of its source, so nothing is copied, but only APFS can
// make one.
//
// A process that is killed between making the clone and removing it leaves the clone behind.
func reopenSpoolFile(file *os.File) (*os.File, error) {
	conn, err := file.SyscallConn()
	if err != nil {
		return nil, err
	}
	clone := filepath.Join(filepath.Dir(file.Name()), spoolFilePrefix+rand.Text())
	// Control keeps the descriptor from being closed while it is in use.
	if controlErr := conn.Control(func(fd uintptr) {
		err = unix.Fclonefileat(int(fd), unix.AT_FDCWD, clone, 0)
	}); controlErr != nil {
		return nil, controlErr
	}
	if err != nil {
		return nil, err
	}
	reopened, err := os.Open(clone)
	if err = errors.Join(err, os.Remove(clone)); err != nil {
		if reopened != nil {
			err = errors.Join(err, reopened.Close())
		}
		return nil, err
	}
	return reopened, nil
}
