//go:build unix

package osutil

import (
	"errors"
	"math"
	"os"
	"syscall"
)

// socketDir returns the directory unix domain sockets of plugins are created in.
// Not os.TempDir: on macOS it is long enough to push socket paths past the unix socket path limit.
func socketDir() string {
	return "/tmp"
}

// ProcessAlive reports whether a process with the given PID is running.
// Like the windows implementation it errs towards alive: callers delete the socket and
// lock file of a process reported dead, so only a definitive answer may report one.
func ProcessAlive(pid int) bool {
	// A non-positive PID addresses a process group in kill(2), and a PID beyond pid_t
	// truncates; neither names the process the caller asked about.
	if pid <= 0 || pid > math.MaxInt32 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 performs the existence and permission checks of kill(2) without sending a signal.
	err = process.Signal(syscall.Signal(0))
	// EPERM means the process exists but belongs to someone we may not signal, which happens
	// in a shared socket directory. Only ESRCH states that no process holds this PID.
	return err == nil || !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone)
}
