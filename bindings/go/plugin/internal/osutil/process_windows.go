package osutil

import (
	"errors"
	"math"
	"os"

	"golang.org/x/sys/windows"
)

// socketDir returns the directory unix domain sockets of plugins are created in.
func socketDir() string {
	return os.TempDir()
}

// openProcess opens a handle to the process with the given PID for waiting on it.
// A PID outside the range Windows uses has no process, reported as ERROR_INVALID_PARAMETER
// like a PID that is simply unused.
func openProcess(pid int) (windows.Handle, error) {
	if pid <= 0 || int64(pid) > math.MaxUint32 {
		return 0, windows.ERROR_INVALID_PARAMETER
	}
	return windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
}

// ProcessAlive reports whether a process with the given PID is running.
// os.Process.Signal only supports Kill on Windows, so liveness is checked on the process handle.
func ProcessAlive(pid int) bool {
	handle, err := openProcess(pid)
	if err != nil {
		// Only ERROR_INVALID_PARAMETER states that no process holds this PID. Any other
		// error (access denied, out of handles) says nothing about the process, and callers
		// delete the socket and lock file of a process reported dead: assume it is alive.
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	// A handle to an exited process stays valid while anyone holds it; it is signaled once the process exited.
	event, err := windows.WaitForSingleObject(handle, 0)
	return err != nil || event != windows.WAIT_OBJECT_0
}
