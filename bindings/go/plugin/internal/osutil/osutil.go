// Package osutil contains the operating system specific details of running plugins
// that are shared between the plugin SDK and the plugin manager.
package osutil

import (
	"os"
	"path/filepath"
	"runtime"
)

// SocketPath returns the unix domain socket location of the plugin with the given ID.
func SocketPath(id string) string {
	name := id + "-plugin.socket"
	if runtime.GOOS == "windows" {
		return filepath.Join(os.TempDir(), name)
	}
	// Not os.TempDir: on macOS it is long enough to push socket paths past the unix socket path limit.
	return "/tmp/" + name
}

// ExecutableName returns the file name of an executable with the given base name on the current OS.
func ExecutableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
