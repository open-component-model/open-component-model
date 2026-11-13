package plugins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// exitGracePeriod is how long a plugin asked to shut down may take to exit before it is killed.
const exitGracePeriod = 2 * time.Second

// Process is a started plugin process together with the connection used to talk to it.
type Process struct {
	cmd      *exec.Cmd
	client   *http.Client
	typ      types.ConnectionType
	location string
}

// NewProcess returns the Process of a plugin that was started and waited for with WaitForPlugin.
func NewProcess(plugin *types.Plugin, client *http.Client, location string) Process {
	return Process{
		cmd:      plugin.Cmd,
		client:   client,
		typ:      plugin.Config.Type,
		location: location,
	}
}

// Kill terminates the plugin process immediately.
func (p Process) Kill() error {
	return p.cmd.Process.Kill()
}

// Wait waits for the plugin process to exit.
func (p Process) Wait() error {
	_, err := p.cmd.Process.Wait()
	return err
}

// Shutdown asks the plugin to terminate and waits up to exitGracePeriod for it to exit,
// killing it if the grace period expires or ctx is done.
// A plugin that already exited is a successful shutdown.
func (p Process) Shutdown(ctx context.Context) error {
	interruptErr := p.Interrupt(ctx)
	if errors.Is(interruptErr, os.ErrProcessDone) {
		return nil
	}
	// A failed request is not yet a failed shutdown: an exiting plugin stops serving
	// before it is gone, and on Windows the request is the only way to ask at all.
	// Wait for the process either way and only report the failure if it has to be killed.

	exited := make(chan error, 1)
	go func() { exited <- p.Wait() }()

	timer := time.NewTimer(exitGracePeriod)
	defer timer.Stop()

	select {
	case err := <-exited:
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	case <-timer.C:
	case <-ctx.Done():
	}

	// On Windows, killing an already exited process causes access denied instead of os.ErrProcessDone.
	killErr := p.Kill()
	select {
	case <-exited:
	case <-time.After(exitGracePeriod):
		return errors.Join(interruptErr, killErr, fmt.Errorf("plugin %d did not exit after being killed", p.cmd.Process.Pid))
	}
	if interruptErr != nil {
		slog.WarnContext(ctx, "plugin did not accept the shutdown request and was killed",
			"pid", p.cmd.Process.Pid, "error", interruptErr)
	}
	return nil
}

// ShutdownAll shuts down all given processes concurrently.
// A plugin failing to shut down must not cancel and kill its siblings, so the group has no context.
func ShutdownAll(ctx context.Context, processes []Process) error {
	var group errgroup.Group
	for _, process := range processes {
		group.Go(func() error {
			return process.Shutdown(ctx)
		})
	}
	return group.Wait()
}
