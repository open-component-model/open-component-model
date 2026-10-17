package plugins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"ocm.software/open-component-model/bindings/go/plugin/internal/osutil"
)

const (
	// interruptTimeout bounds the request asking a plugin to shut down. The plugin answers before shutting down.
	interruptTimeout = 5 * time.Second
	// exitGracePeriod is how long an unreachable plugin may take to finish exiting before it is killed.
	exitGracePeriod = 2 * time.Second
)

// Interrupt asks the plugin to shut down gracefully through its shutdown endpoint, which the
// plugin SDK serves, because Windows cannot deliver os.Interrupt to another process.
// An unreachable plugin that does not exit on its own is killed.
// It returns os.ErrProcessDone if the plugin already exited.
func (p Process) Interrupt(ctx context.Context) error {
	// Like sending a signal, delivering the request must not fail because the caller's context is already done.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), interruptTimeout)
	defer cancel()

	callErr := Call(ctx, p.Client, p.Type, p.Location, "shutdown", http.MethodGet)
	if callErr == nil {
		return nil
	}

	pid := p.Cmd.Process.Pid
	if osutil.WaitForExit(pid, exitGracePeriod) {
		return os.ErrProcessDone
	}
	if err := p.Cmd.Process.Kill(); err != nil {
		// TerminateProcess is denied for a process that is already terminating.
		if errors.Is(err, os.ErrProcessDone) || osutil.WaitForExit(pid, exitGracePeriod) {
			return os.ErrProcessDone
		}
		return errors.Join(fmt.Errorf("failed to request plugin shutdown: %w", callErr), err)
	}

	slog.WarnContext(ctx, "plugin did not accept the shutdown request and was killed", "pid", pid, "error", callErr)
	return nil
}
