package osutil

import (
	"errors"
	"time"

	"golang.org/x/sys/windows"
)

// ProcessAlive reports whether a process with the given PID is running.
// os.Process.Signal only supports Kill on Windows, so liveness is checked on the process handle.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// The process exists, but belongs to someone we may not inspect.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	// A handle to an exited process stays valid while anyone holds it; it is signaled once the process exited.
	event, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && event == uint32(windows.WAIT_TIMEOUT)
}

// WaitForExit waits up to timeout for the process with the given PID to exit and reports whether it exited.
// Termination is asynchronous on Windows, so a killed process can still be running briefly.
func WaitForExit(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return true
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		// No process with that PID exists anymore.
		return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	event, err := windows.WaitForSingleObject(handle, uint32(timeout.Milliseconds()))
	return err == nil && event == windows.WAIT_OBJECT_0
}
