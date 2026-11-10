//go:build unix

package osutil

import (
	"os"
	"syscall"
)

// socketDir returns the directory unix domain sockets of plugins are created in.
// Not os.TempDir: on macOS it is long enough to push socket paths past the unix socket path limit.
func socketDir() string {
	return "/tmp"
}

// ProcessAlive reports whether a process with the given PID is running.
func ProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 performs the existence and permission checks of kill(2) without sending a signal.
	return process.Signal(syscall.Signal(0)) == nil
}
