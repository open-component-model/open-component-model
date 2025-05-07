package sdk

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func TestPluginSDK(t *testing.T) {
	r := require.New(t)

	output := bytes.NewBuffer(nil)
	location := "/tmp/test-plugin-plugin.socket"
	ctx := context.Background()
	p := NewPlugin(ctx, types.Config{
		ID:         "test-plugin",
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
	location := "/tmp/test-plugin-plugin.socket"
	ctx := context.Background()
	baseCtx := context.Background()
	p := NewPlugin(baseCtx, types.Config{
		ID:         "test-plugin",
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
	location := "/tmp/test-plugin-plugin.socket"
	output := bytes.NewBuffer(nil)
	timeout := 10 * time.Millisecond
	ctx := context.Background()
	p := NewPlugin(ctx, types.Config{
		ID:          "test-plugin",
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

		r.ErrorContains(err, "dial unix /tmp/test-plugin-plugin.socket: connect: no such file or directory")

		return true
	}, 5*time.Second, 20*time.Millisecond)
}

func TestHealthCheckInvalidMethod(t *testing.T) {
	r := require.New(t)
	location := "/tmp/test-plugin-plugin.socket"
	output := bytes.NewBuffer(nil)
	ctx := context.Background()
	p := NewPlugin(ctx, types.Config{
		ID:         "test-plugin",
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
