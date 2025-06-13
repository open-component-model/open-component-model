package plugins

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

const connectionTimeout = 30 * time.Second

type KV struct {
	Key   string
	Value string
}

// WaitForPlugin sets up the HTTP client for the plugin and waits for it to become available.
// It returns the configured HTTP client, the plugin location, and any error encountered.
func WaitForPlugin(ctx context.Context, plugin *types.Plugin) (*http.Client, string, error) {
	interval := 100 * time.Millisecond
	timer := time.NewTicker(interval)
	timeout := 5 * time.Second

	location, err := getPluginLocation(ctx, plugin)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get plugin location: %w", err)
	}

	slog.DebugContext(ctx, "got plugin location", "location", location)

	client, err := connect(ctx, plugin.ID, location, plugin.Config.Type)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to plugin %s: %w", plugin.ID, err)
	}

	base := "http://unix"
	if plugin.Config.Type == types.TCP {
		// if the type is TCP the location will include the port
		base = location
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to plugin %s: %w", plugin.ID, err)
	}

	for {
		// This is the main work of the loop that we want to execute at least once
		// right away.
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()

			return client, location, nil
		}

		select {
		case <-timer.C:
			// tick the loop and repeat the main loop body every set interval
		case <-time.After(timeout):
			return nil, "", fmt.Errorf("timed out waiting for plugin %s", plugin.ID)
		case <-ctx.Done():
			return nil, "", fmt.Errorf("context was cancelled %s", plugin.ID)
		}
	}
}

func getPluginLocation(ctx context.Context, plugin *types.Plugin) (string, error) {
	if plugin.Stdout == nil {
		return "", errors.New("communication channel with the plugin is not set up; stdout is nil")
	}

	location := make(chan string, 1)
	errChan := make(chan error, 1)

	timeoutCtx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()

	// Create a scanner to read output line by line
	scanner := bufio.NewScanner(plugin.Stdout)

	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			slog.DebugContext(ctx, "server output:", "line", line)
			switch {
			case strings.HasPrefix(line, "http://"):
				location <- line

				// stop the go routine, we have what we came for
				return
			case strings.HasPrefix(line, "http+unix://"):
				location <- strings.TrimPrefix(line, "http+unix://")

				// stop the go routine, we have what we came for
				return
			default:
				slog.DebugContext(ctx, "skipping line with unknown scheme", "line", line)
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("error reading server output: %w", err)
		}
	}()

	// Wait for either the location, an error, or timeout
	select {
	case loc := <-location:
		return loc, nil
	case err := <-errChan:
		return "", err
	case <-timeoutCtx.Done():
		return "", fmt.Errorf("timed out waiting for server to start")
	}
}

// connect will create a client that sets up connection based on the plugin's connection type.
// That is either a Unix socket or a TCP based connection. It does this by setting the `DialContext` using
// the right network location.
func connect(_ context.Context, id, location string, typ types.ConnectionType) (*http.Client, error) {
	var network string
	switch typ {
	case types.Socket:
		network = "unix"
	case types.TCP:
		network = "tcp"
		location = strings.TrimPrefix(location, "http://")
	default:
		return nil, fmt.Errorf("invalid connection type: %s", typ)
	}

	dialer := net.Dialer{
		Timeout: 30 * time.Second,
	}

	// Create an HTTP client with the Unix socket connection
	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, location)
				if err != nil {
					return nil, fmt.Errorf("failed to connect to plugin %s: %w", id, err)
				}

				return conn, nil
			},
		},
	}

	return client, nil
}
