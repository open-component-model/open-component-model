//go:build !windows

package plugins

import (
	"context"
	"os"
)

// Interrupt asks the plugin to shut down gracefully by sending it os.Interrupt,
// which plugins are expected to handle. For Go, this is done by the plugin SDK.
func (p Process) Interrupt(_ context.Context) error {
	return p.Cmd.Process.Signal(os.Interrupt)
}
