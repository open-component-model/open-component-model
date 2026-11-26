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

// stillActive is the exit code GetExitCodeProcess reports while a process runs. It is a
// legal exit code as well, so it only ever says "ask the process handle instead".
const stillActive = 259

// openProcess opens a handle to the process with the given PID to inspect its state.
// A PID outside the range Windows uses has no process, reported as ERROR_INVALID_PARAMETER
// like a PID that is simply unused.
func openProcess(pid int) (windows.Handle, error) {
	if pid <= 0 || int64(pid) > math.MaxUint32 {
		return 0, windows.ERROR_INVALID_PARAMETER
	}
	return windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
}

// ProcessAlive reports whether a process with the given PID is running.
// os.Process.Signal only supports Kill on Windows, so liveness is read off the process handle.
func ProcessAlive(pid int) bool {
	handle, err := openProcess(pid)
	if err != nil {
		// Only ERROR_INVALID_PARAMETER states that no process holds this PID. Any other
		// error (access denied, out of handles) says nothing about the process, and callers
		// delete the socket and lock file of a process reported dead: assume it is alive.
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	// A process that is on its way out has its exit code set before its handle is signaled,
	// so the handle alone reports it as running for as long as the teardown takes.
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err == nil && code != stillActive {
		return false
	}

	// The exit code is unavailable or ambiguous: a handle to an exited process stays valid
	// while anyone holds it and is signaled once the process is gone.
	event, err := windows.WaitForSingleObject(handle, 0)
	return err != nil || event != windows.WAIT_OBJECT_0
}

// Kill terminates the process, reporting os.ErrProcessDone for a process that already exited.
// TerminateProcess fails with ERROR_ACCESS_DENIED once the process is gone, which a caller
// cannot tell apart from a process it may not terminate, so the process itself decides.
func Kill(process *os.Process) error {
	err := process.Kill()
	if err == nil || !errors.Is(err, windows.ERROR_ACCESS_DENIED) || ProcessAlive(process.Pid) {
		return err
	}
	return os.ErrProcessDone
}
