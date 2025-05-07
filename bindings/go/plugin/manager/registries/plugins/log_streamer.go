package plugins

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

type pluginIDString string

const pluginID pluginIDString = "pluginID"

// StartLogStreamer is intended to be launched when the plugin is started. It will continuously stream logs
// from the plugin to the context debug logger. Logs in the plugin has to be written to stderr.
func StartLogStreamer(ctx context.Context, plugin *types.Plugin) {
	if plugin.Stderr == nil {
		return
	}

	ctx = context.WithValue(ctx, pluginID, plugin.ID)

	// Create a scanner to read output line by line
	scanner := bufio.NewScanner(plugin.Stderr)

	lineChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Start a goroutine to scan for the port number
	go func() {
		for scanner.Scan() {
			lineChan <- scanner.Text()
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("error reading server output: %w", err)
		}
	}()

	// start streaming log messages to the debug context log
	for {
		select {
		case line := <-lineChan:
			slog.DebugContext(ctx, line)
		case err := <-errChan:
			slog.DebugContext(ctx, "streaming logs from plugin failed", "error", err.Error())
		case <-ctx.Done():
			// context is done, we stop streaming logs
			slog.DebugContext(ctx, "stopping log streamer, context is done")
			return
		}
	}
}
