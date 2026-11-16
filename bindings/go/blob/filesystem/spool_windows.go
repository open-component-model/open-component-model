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

var procReOpenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

// reopenSpoolFile returns another handle to the spool file with its own read position. A
// file that is deleted on close can only be opened again by a handle that shares delete
// access, which os.Open does not offer, so the new handle is derived from the open one.
func reopenSpoolFile(file *os.File) (reopened *os.File, err error) {
	if err := procReOpenFile.Find(); err != nil {
		return nil, err
	}
	conn, err := file.SyscallConn()
	if err != nil {
		return nil, err
	}
	// Control keeps the handle from being closed while it is in use.
	if controlErr := conn.Control(func(handle uintptr) {
		const share = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
		h, _, callErr := procReOpenFile.Call(handle, windows.GENERIC_READ, share, 0)
		if windows.Handle(h) == windows.InvalidHandle {
			err = callErr
			return
		}
		reopened = os.NewFile(h, file.Name())
	}); controlErr != nil {
		return nil, controlErr
	}
	return reopened, err
}
