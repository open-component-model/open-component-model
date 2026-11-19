// Package osutil contains the operating system specific details of running plugins
// that are shared between the plugin SDK and the plugin manager.
package osutil

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// ConnectionType returns the transport plugins should use: unix domain sockets
// where the system can create them, otherwise TCP on loopback. Scratch
// containers without a writable temp dir, Windows before AF_UNIX support, and
// Wine fall back to TCP.
func ConnectionType(ctx context.Context) types.ConnectionType {
	tmp, err := os.MkdirTemp("", "")
	if err != nil {
		// An unexpected TCP plugin is the first thing an operator looks for, so name the reason.
		slog.DebugContext(ctx, "falling back to TCP: no temporary directory for a unix socket", "error", err)
		return types.TCP
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "unix", filepath.Join(tmp, "plugin.sock"))
	if err != nil {
		slog.DebugContext(ctx, "falling back to TCP: unix sockets are not supported", "error", err)
		return types.TCP
	}
	_ = listener.Close()
	return types.Socket
}

// SocketPath returns the unix domain socket location of the plugin with the given ID.
func SocketPath(id string) string {
	return filepath.Join(socketDir(), id+"-plugin.socket")
}
