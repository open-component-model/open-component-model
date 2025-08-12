// Package main_test provides comprehensive integration tests for the helm input plugin.
// These tests validate the plugin's lifecycle, capabilities, resource processing,
// and error handling by running the actual binary and communicating over Unix sockets.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	helmv1 "ocm.software/open-component-model/bindings/go/input/helm/spec/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	testPluginPath = "../tmp/testdata/test-input-plugin"
	testSocketPath = "/tmp/test-helm-input-plugin.socket"
)

func TestHelmPluginCapabilities(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Remove(testSocketPath)
	})

	cmd := exec.Command(testPluginPath, "capabilities")
	output, err := cmd.Output()
	require.NoError(t, err, "capabilities command should succeed")

	var capabilities mtypes.Types
	err = json.Unmarshal(output, &capabilities)
	require.NoError(t, err, "capabilities output should be valid JSON")
	require.Contains(t, capabilities.Types, mtypes.InputPluginType, "should contain input plugin type")

	helmTypes := capabilities.Types[mtypes.InputPluginType]
	require.Len(t, helmTypes, 1, "should have exactly one helm input type")

	helmType := helmTypes[0]
	require.Equal(t, helmv1.Type, helmType.Type.Name, "type name should be 'helm'")
	require.Equal(t, helmv1.Version, helmType.Type.Version, "type version should be 'v1'")
}

func TestHelmPluginLifecycle(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Remove(testSocketPath)
	})

	config := mtypes.Config{
		ID:   "test-helm-input",
		Type: mtypes.Socket,
	}
	configData, err := json.Marshal(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, testPluginPath, "--config", string(configData))

	// Set up pipes for communication
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)

	err = cmd.Start()
	require.NoError(t, err)

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	require.Eventually(t, func() bool {
		_, err := os.Stat(testSocketPath)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "plugin socket should be created")

	// ping the plugin
	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return net.Dial("unix", testSocketPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "http://unix/healthz", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "healthz endpoint should return 200")

	go func() {
		io.Copy(os.Stderr, stderr)
	}()

	go func() {
		io.Copy(os.Stdout, stdout)
	}()
}

func TestHelmPluginProcessResource(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Remove(testSocketPath)
	})

	config := mtypes.Config{
		ID:   "test-helm-input",
		Type: mtypes.Socket,
	}
	configData, err := json.Marshal(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, testPluginPath, "--config", string(configData))

	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)

	err = cmd.Start()
	require.NoError(t, err)

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	require.Eventually(t, func() bool {
		_, err := os.Stat(testSocketPath)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond)

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return net.Dial("unix", testSocketPath)
			},
		},
		Timeout: 10 * time.Second,
	}

	chartPath, err := filepath.Abs("../testdata/mychart")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(chartPath, "Chart.yaml"))
	require.NoError(t, err, "test chart should exist")

	helmInput := &runtime.Raw{
		Type: runtime.Type{
			Name:    helmv1.Type,
			Version: helmv1.Version,
		},
		Data: func() []byte {
			data, _ := json.Marshal(helmv1.Helm{
				Type: runtime.Type{
					Name:    helmv1.Type,
					Version: helmv1.Version,
				},
				Path: chartPath,
			})
			return data
		}(),
	}

	request := &v1.ProcessResourceInputRequest{
		Resource: &constructorv1.Resource{
			ElementMeta: constructorv1.ElementMeta{
				ObjectMeta: constructorv1.ObjectMeta{
					Name:    "test-helm-chart",
					Version: "0.1.0",
				},
			},
			Type:     "helmChart",
			Relation: "local",
			AccessOrInput: constructorv1.AccessOrInput{
				Input: helmInput,
			},
		},
	}

	requestBody, err := json.Marshal(request)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, "POST", "http://unix/resource/process", bytes.NewReader(requestBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", `{"access_token": "test"}`) // Required by the handler

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusOK {
		t.Logf("Error response (status %d): %s", resp.StatusCode, string(responseBody))
	}

	require.Equal(t, http.StatusOK, resp.StatusCode, "process resource should succeed")

	var response v1.ProcessResourceInputResponse
	err = json.Unmarshal(responseBody, &response)
	require.NoError(t, err, "response should be valid JSON")

	require.NotNil(t, response.Resource, "response should contain resource")
	require.Equal(t, "helmChart", response.Resource.Type, "resource type should be helmChart")
	require.NotNil(t, response.Location, "response should contain location")
	require.Equal(t, mtypes.LocationTypeLocalFile, response.Location.LocationType, "location should be local file")
	require.NotEmpty(t, response.Location.Value, "location value should not be empty")
	generatedFile := response.Location.Value
	require.FileExists(t, generatedFile, "generated helm chart file should exist")

	file, err := os.Open(generatedFile)
	require.NoError(t, err)
	defer file.Close()

	header := make([]byte, 2)
	_, err = file.Read(header)
	require.NoError(t, err)
	require.Equal(t, []byte{0x1f, 0x8b}, header, "file should be gzip compressed (tar.gz)")

	go func() {
		io.Copy(os.Stderr, stderr)
	}()
	go func() {
		io.Copy(os.Stdout, stdout)
	}()
}

func TestHelmPluginProcessResourceWithInvalidInput(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Remove(testSocketPath)
	})

	// Create plugin config
	config := mtypes.Config{
		ID:   "test-helm-input",
		Type: mtypes.Socket,
	}
	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Start plugin
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, testPluginPath, "--config", string(configData))

	err = cmd.Start()
	require.NoError(t, err)

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	// Wait for plugin to be ready
	require.Eventually(t, func() bool {
		_, err := os.Stat(testSocketPath)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond)

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return net.Dial("unix", testSocketPath)
			},
		},
		Timeout: 10 * time.Second,
	}

	// Test with non-existent chart path
	helmInput := &runtime.Raw{
		Type: runtime.Type{
			Name:    helmv1.Type,
			Version: helmv1.Version,
		},
		Data: func() []byte {
			data, _ := json.Marshal(helmv1.Helm{
				Type: runtime.Type{
					Name:    helmv1.Type,
					Version: helmv1.Version,
				},
				Path: "/non/existent/path",
			})
			return data
		}(),
	}

	request := &v1.ProcessResourceInputRequest{
		Resource: &constructorv1.Resource{
			ElementMeta: constructorv1.ElementMeta{
				ObjectMeta: constructorv1.ObjectMeta{
					Name:    "test-helm-chart",
					Version: "0.1.0",
				},
			},
			Type:     "helmChart",
			Relation: "local",
			AccessOrInput: constructorv1.AccessOrInput{
				Input: helmInput,
			},
		},
	}

	requestBody, err := json.Marshal(request)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, "POST", "http://unix/resource/process", bytes.NewReader(requestBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", `{"access_token": "test"}`)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.NotEqual(t, http.StatusOK, resp.StatusCode, "processing invalid chart should fail")
}

func TestHelmPluginProcessSource(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Remove(testSocketPath)
	})

	// Create plugin config
	config := mtypes.Config{
		ID:   "test-helm-input",
		Type: mtypes.Socket,
	}
	configData, err := json.Marshal(config)
	require.NoError(t, err)

	// Start plugin
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, testPluginPath, "--config", string(configData))
	err = cmd.Start()
	require.NoError(t, err)

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	require.Eventually(t, func() bool {
		_, err := os.Stat(testSocketPath)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond)

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return net.Dial("unix", testSocketPath)
			},
		},
		Timeout: 10 * time.Second,
	}

	request := &v1.ProcessSourceInputRequest{
		Source: &constructorv1.Source{
			ElementMeta: constructorv1.ElementMeta{
				ObjectMeta: constructorv1.ObjectMeta{
					Name:    "test-helm-source",
					Version: "0.1.0",
				},
			},
			Type: "helmChart",
		},
	}

	requestBody, err := json.Marshal(request)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, "POST", "http://unix/source/process", bytes.NewReader(requestBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", `{"access_token": "test"}`)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.True(t, resp.StatusCode >= 400, "process source should return an error status code since it's not implemented")
}
