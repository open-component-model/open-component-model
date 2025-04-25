package plugins

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

type KV struct {
	Key   string
	Value string
}

// WaitForPlugin sets up the HTTP client for the plugin or returns it, based on how I'm going to extract this.
func WaitForPlugin(ctx context.Context, id, location string, typ types.ConnectionType) (*http.Client, error) {
	interval := 100 * time.Millisecond
	timer := time.NewTicker(interval)
	timeout := 5 * time.Second

	client, err := connect(ctx, id, location, typ)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to plugin %s: %w", id, err)
	}

	base := "http://unix"
	if typ == types.TCP {
		// if the type is TCP the location will include the port
		base = location
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to plugin %s: %w", id, err)
	}

	for {
		// This is the main work of the loop that we want to execute at least once
		// right away.
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()

			return client, nil
		}

		select {
		case <-timer.C:
			// tick the loop and repeat the main loop body every set interval
		case <-time.After(timeout):
			return nil, fmt.Errorf("timed out waiting for plugin %s", id)
		case <-ctx.Done():
			return nil, fmt.Errorf("context was cancelled %s", id)
		}
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
