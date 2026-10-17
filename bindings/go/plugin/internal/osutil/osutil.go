// Package osutil contains the operating system specific details of running plugins
// that are shared between the plugin SDK and the plugin manager.
package osutil

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// ConnectionType returns the transport plugins should use: unix domain sockets
// where the system can create them, otherwise TCP on loopback. Scratch
// containers without a writable temp dir, Windows before AF_UNIX support, and
// Wine fall back to TCP.
func ConnectionType(ctx context.Context) types.ConnectionType {
	tmp, err := os.MkdirTemp("", "")
	if err != nil {
		return types.TCP
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "unix", filepath.Join(tmp, "plugin.sock"))
	if err != nil {
		return types.TCP
	}
	_ = listener.Close()
	return types.Socket
}

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
