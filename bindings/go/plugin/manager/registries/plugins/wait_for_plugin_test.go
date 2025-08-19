package plugins

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func TestWaitForPlugin(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	t.Run("successful connection to TCP plugin", func(t *testing.T) {
		output := bytes.NewBuffer(nil)
		// Start a test server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" {
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()
		p := &types.Plugin{
			ID: "test-tcp-plugin",
			Config: types.Config{
				Type: types.TCP,
			},
			Stdout: io.NopCloser(output),
		}
		output.Write([]byte(server.URL))

		ctx := context.Background()
		client, _, err := WaitForPlugin(ctx, p)

		require.NoError(t, err)
		require.NotNil(t, client)

		// Verify the client works
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/healthz", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("successful connection to Unix socket plugin", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "test-unix-socket")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		socketPath := filepath.Join(tempDir, "test.sock")

		// Start a Unix socket server
		var lc net.ListenConfig
		listener, err := lc.Listen(t.Context(), "unix", socketPath)
		require.NoError(t, err)
		defer listener.Close()

		// Create a simple HTTP server that handles /healthz
		server := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/healthz" {
					w.WriteHeader(http.StatusOK)
				}
			}),
		}

		// Start the server in a goroutine
		go func() {
			_ = server.Serve(listener)
		}()
		defer server.Close()
		output := bytes.NewBuffer(nil)
		p := &types.Plugin{
			ID: "test-socket-plugin",
			Config: types.Config{
				Type: types.Socket,
			},
			Stdout: io.NopCloser(output),
		}
		output.Write([]byte("http+unix://" + socketPath))
		// Test the WaitForPlugin function
		client, _, err := WaitForPlugin(t.Context(), p)

		require.NoError(t, err)
		require.NotNil(t, client)
	})

	t.Run("context cancellation", func(t *testing.T) {
		// Create a context that's already canceled
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		p := &types.Plugin{
			ID: "test-plugin",
			Config: types.Config{
				Type: types.Socket,
			},
			Stdout: io.NopCloser(bytes.NewBuffer(nil)),
		}

		// This should fail immediately due to context cancellation
		client, _, err := WaitForPlugin(ctx, p)

		assert.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "timed out waiting for server to start")
	})

	t.Run("invalid connection type", func(t *testing.T) {
		ctx := context.Background()
		// Use a connection type that's not handled
		buffer := bytes.NewBuffer(nil)
		p := &types.Plugin{
			ID: "test-invalid-plugin",
			Config: types.Config{
				Type: types.ConnectionType("999"),
			},
			Stdout: io.NopCloser(buffer),
		}
		buffer.Write([]byte("http+unix://dummy"))
		client, _, err := WaitForPlugin(ctx, p)

		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestConnect(t *testing.T) {
	t.Run("socket connection", func(t *testing.T) {
		ctx := context.Background()
		client, err := connect(ctx, "test-socket", "/path/to/socket", types.Socket)

		require.NoError(t, err)
		require.NotNil(t, client)

		// Check that we have the right configuration in transport
		transport, ok := client.Transport.(*http.Transport)
		require.True(t, ok)

		// Check that the dialer is configured
		require.NotNil(t, transport.DialContext)
	})

	t.Run("TCP connection", func(t *testing.T) {
		ctx := context.Background()
		client, err := connect(ctx, "test-tcp", "localhost:8080", types.TCP)

		require.NoError(t, err)
		require.NotNil(t, client)

		// Check that we have the right configuration in transport
		transport, ok := client.Transport.(*http.Transport)
		require.True(t, ok)

		// Check that the dialer is configured
		require.NotNil(t, transport.DialContext)
	})

	t.Run("connection attempt with socket type", func(t *testing.T) {
		// Don't expect this to succeed, but verify it attempts to connect with unix network
		ctx := context.Background()
		client, err := connect(ctx, "test-socket", "/non/existent/socket", types.Socket)

		require.NoError(t, err)
		require.NotNil(t, client)

		// Create a test request
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/test", nil)
		require.NoError(t, err)

		// This should fail because the socket doesn't exist, but we're testing the connection attempt
		_, err = client.Do(req)
		assert.Error(t, err)
		// Check that the error mentions unix socket
		assert.Contains(t, err.Error(), "/non/existent/socket")
	})

	t.Run("connection attempt with TCP type", func(t *testing.T) {
		// Don't expect this to succeed, but verify it attempts to connect with tcp network
		ctx := context.Background()
		client, err := connect(ctx, "test-tcp", "localhost:12345", types.TCP)

		require.NoError(t, err)
		require.NotNil(t, client)

		// Create a test request
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:12345/test", nil)
		require.NoError(t, err)

		// This should fail because port 12345 likely isn't open, but we're testing the connection attempt
		_, err = client.Do(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "localhost:12345")
	})
}
