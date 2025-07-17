package sdk

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func TestPluginSDK(t *testing.T) {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	r := require.New(t)

	output := bytes.NewBuffer(nil)
	location := "/tmp/test-plugin-flow-plugin.socket"
	ctx := context.Background()
	p := NewPlugin(ctx, slog.Default(), types.Config{
		ID:         "test-plugin-flow",
		Type:       types.Socket,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, output)

	t.Cleanup(func() {
		r.NoError(os.RemoveAll(location))
	})

	r.NoError(p.RegisterHandlers(endpoints.Handler{
		Handler: func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte("hello"))
		},
		Location: "/test-location",
	}))

	go func() {
		_ = p.Start(ctx)
	}()

	httpClient := createHttpClient(location)

	// Health check endpoint should be added automatically.
	waitForPlugin(r, httpClient)

	resp, err := httpClient.Get("http://unix/test-location")
	r.NoError(err)
	content, err := io.ReadAll(resp.Body)
	r.NoError(err)
	r.Equal("hello", string(content))

	// Shutdown endpoint should be added automatically.
	r.NoError(p.GracefulShutdown(ctx))

	// GracefulShutdown should remove the socket.
	_, err = os.Stat(location)
	r.True(os.IsNotExist(err))
}

func TestPluginSDKForceShutdownContext(t *testing.T) {
	r := require.New(t)

	output := bytes.NewBuffer(nil)
	location := "/tmp/test-plugin-force-plugin.socket"
	ctx := context.Background()
	baseCtx := context.Background()
	p := NewPlugin(baseCtx, slog.Default(), types.Config{
		ID:         "test-plugin-force",
		Type:       types.Socket,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, output)

	t.Cleanup(func() {
		r.NoError(os.RemoveAll(location))
	})

	r.NoError(p.RegisterHandlers(endpoints.Handler{
		Handler: func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte("hello"))
		},
		Location: "/test-location",
	}))

	go func() {
		_ = p.Start(ctx)
	}()

	httpClient := createHttpClient(location)

	// Health check endpoint should be added automatically.
	waitForPlugin(r, httpClient)

	resp, err := httpClient.Get("http://unix/test-location")
	r.NoError(err)
	content, err := io.ReadAll(resp.Body)
	r.NoError(err)
	r.Equal("hello", string(content))

	// Shutdown endpoint should be added automatically.
	forceCTX, cancel := context.WithTimeout(context.Background(), time.Second)
	parse, err := url.Parse("http://unix/shutdown")
	r.NoError(err)
	req := &http.Request{
		Method: "GET",
		URL:    parse,
	}
	req = req.WithContext(forceCTX)

	_, err = httpClient.Do(req)
	// The above cancel doesn't kill the shutdown process
	r.Error(err)
	cancel()
	r.Eventually(func() bool {
		_, err := httpClient.Get("http://unix/healthz")

		return err != nil
	}, 10*time.Second, 5*time.Millisecond)
}

func TestIdleChecker(t *testing.T) {
	r := require.New(t)
	location := "/tmp/test-plugin-idle-plugin.socket"
	output := bytes.NewBuffer(nil)
	timeout := 10 * time.Millisecond
	ctx := context.Background()
	p := NewPlugin(ctx, slog.Default(), types.Config{
		ID:          "test-plugin-idle",
		Type:        types.Socket,
		PluginType:  types.ComponentVersionRepositoryPluginType,
		IdleTimeout: &timeout,
	}, output)

	t.Cleanup(func() {
		r.NoError(os.RemoveAll(location))
	})

	go func() {
		_ = p.Start(ctx)
	}()
	// wait until the plugin starts up
	r.Eventually(func() bool {
		if p.server == nil {
			return false
		}

		return true
	}, time.Second, 5*time.Millisecond)

	httpClient := createHttpClient(location)

	// idle timeout should kill the plugin and remove the socket prematurely.
	r.Eventually(func() bool {
		_, err := httpClient.Get("http://unix/healthz")
		if err == nil {
			return false
		}

		r.ErrorContains(err, "dial unix /tmp/test-plugin-idle-plugin.socket: connect: no such file or directory")

		return true
	}, 5*time.Second, 20*time.Millisecond)
}

func TestHealthCheckInvalidMethod(t *testing.T) {
	r := require.New(t)
	location := "/tmp/test-plugin-invalid-plugin.socket"
	output := bytes.NewBuffer(nil)
	ctx := context.Background()
	p := NewPlugin(ctx, slog.Default(), types.Config{
		ID:         "test-plugin-invalid",
		Type:       types.Socket,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, output)

	t.Cleanup(func() {
		r.NoError(os.RemoveAll(location))
	})
	go func() {
		_ = p.Start(ctx)
	}()
	// wait until the plugin starts up
	httpClient := createHttpClient(location)

	// Health check endpoint should be added automatically.
	waitForPlugin(r, httpClient)

	// idle timeout should kill the plugin and remove the socket prematurely.
	resp, err := httpClient.Post("http://unix/healthz", "application/json", bytes.NewBufferString("hello"))
	r.NoError(err)
	r.Equal(http.StatusMethodNotAllowed, resp.StatusCode)
	content, err := io.ReadAll(resp.Body)
	r.NoError(err)
	r.Contains(string(content), "this endpoint may only be called with either HEAD or GET method")
}

func TestPanicRecovery(t *testing.T) {
	r := require.New(t)
	location := "/tmp/test-plugin-panic-plugin.socket"
	output := bytes.NewBuffer(nil)
	ctx := context.Background()
	p := NewPlugin(ctx, slog.Default(), types.Config{
		ID:         "test-plugin-panic",
		Type:       types.Socket,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, output)

	t.Cleanup(func() {
		r.NoError(os.RemoveAll(location))
	})

	r.NoError(p.RegisterHandlers(endpoints.Handler{
		Handler: func(writer http.ResponseWriter, request *http.Request) {
			panic("test panic")
		},
		Location: "/panic-endpoint",
	}))

	go func() {
		_ = p.Start(ctx)
	}()

	httpClient := createHttpClient(location)
	waitForPlugin(r, httpClient)

	resp, err := httpClient.Get("http://unix/panic-endpoint")
	r.NoError(err)
	r.Equal(http.StatusInternalServerError, resp.StatusCode)
	content, err := io.ReadAll(resp.Body)
	r.NoError(err)
	r.Contains(string(content), "panic recovered")

	r.NoError(p.GracefulShutdown(ctx))
}

func waitForPlugin(r *require.Assertions, httpClient *http.Client) {
	r.Eventually(func() bool {
		resp, err := httpClient.Get("http://unix/healthz")
		if err != nil {
			return false
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return false
		}

		return true
	}, 5*time.Second, 20*time.Millisecond)
}

func TestLockFileCreationAndCleanup(t *testing.T) {
	r := require.New(t)
	location := "/tmp/test-plugin-lock-plugin.socket"
	lockFile := location + ".lock"

	t.Cleanup(func() {
		_ = os.RemoveAll(location)
		_ = os.RemoveAll(lockFile)
	})

	// Test lock file creation and PID verification
	ctx := context.Background()
	p := NewPlugin(ctx, slog.Default(), types.Config{
		ID:         "test-plugin-lock",
		Type:       types.Socket,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, os.Stdout)

	// Start the plugin in a goroutine
	go func() {
		_ = p.Start(ctx)
	}()

	// Wait for the plugin to start and create the lock file
	r.Eventually(func() bool {
		_, err := os.Stat(lockFile)
		return err == nil
	}, 5*time.Second, 100*time.Millisecond)

	lockContent, err := os.ReadFile(lockFile)
	r.NoError(err)

	expectedPID := strconv.Itoa(os.Getpid())
	r.Equal(expectedPID, string(lockContent))

	err = p.GracefulShutdown(ctx)
	r.NoError(err)

	_, err = os.Stat(lockFile)
	r.True(os.IsNotExist(err))

	_, err = os.Stat(location)
	r.True(os.IsNotExist(err))
}

func createHttpClient(location string) *http.Client {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", location)
			},
		},
		Timeout: 30 * time.Second,
	}
	return httpClient
}
