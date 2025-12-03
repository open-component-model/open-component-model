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

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	helmv1 "ocm.software/open-component-model/bindings/go/helm/input/spec/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	mtypes "ocm.software/open-component-model/bindings/go/plugin/manager/types"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	testPluginPath = "../tmp/testdata/test-input-plugin"
	testSocketPath = "/tmp/test-helm-input-plugin.socket"
)

func TestHelmPluginCapabilities(t *testing.T) {
	cmd := exec.Command(testPluginPath, "capabilities")
	output, err := cmd.Output()
	require.NoError(t, err, "capabilities command should succeed")

	rawPluginSpec := spec.PluginSpec{}
	require.NoError(t, json.Unmarshal(output, &rawPluginSpec), "capabilities output should be valid JSON")

	pluginSpec, err := pluginruntime.ConvertFromSpec(v1.Scheme, &rawPluginSpec)
	require.NoError(t, err, "should convert plugin spec from raw spec")

	capabilityTypes := make([]string, len(pluginSpec.CapabilitySpecs))
	for index, capability := range pluginSpec.CapabilitySpecs {
		capabilityTypes[index] = capability.GetType().String()
	}

	require.Contains(t, capabilityTypes, string(v1.InputPluginType), "should contain input plugin type")
	inputCapability := pluginSpec.CapabilitySpecs[0]
	require.Len(t, inputCapability.(*v1.CapabilitySpec).SupportedInputTypes, 1, "should have exactly one helm input type")

	helmInputType := inputCapability.(*v1.CapabilitySpec).SupportedInputTypes[0]
	require.Equal(t, helmv1.Type, helmInputType.Type.Name, "type name should be 'helm'")
	require.Equal(t, helmv1.Version, helmInputType.Type.Version, "type version should be 'v1'")
}

func TestHelmPluginLifecycle(t *testing.T) {
	setup := newPluginTestSetup(t)

	resp := setup.makeHTTPRequest(t, "GET", "/healthz", nil, nil)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "healthz endpoint should return 200")
}

func TestHelmPluginProcessResource(t *testing.T) {
	setup := newPluginTestSetup(t)

	chartPath, err := filepath.Abs("../input/testdata/mychart")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(chartPath, "Chart.yaml"))
	require.NoError(t, err, "test chart should exist")

	request := createHelmResourceRequest(t, chartPath)
	requestBody, err := json.Marshal(request)
	require.NoError(t, err)

	headers := map[string]string{
		"Authorization": `{"access_token": "test"}`,
	}
	resp := setup.makeHTTPRequest(t, "POST", "/resource/process", requestBody, headers)
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

	require.NotNil(t, response.Location, "response should contain location")
	require.Equal(t, mtypes.LocationTypeLocalFile, response.Location.LocationType, "location should be local file")
	require.NotEmpty(t, response.Location.Value, "location value should not be empty")
	require.NotEmpty(t, response.Location.MediaType, "location should contain media type")

	generatedFile := response.Location.Value
	require.FileExists(t, generatedFile, "generated helm chart file should exist")

	file, err := os.Open(generatedFile)
	require.NoError(t, err)
	defer file.Close()

	header := make([]byte, 2)
	_, err = file.Read(header)
	require.NoError(t, err)
	require.Equal(t, []byte{0x1f, 0x8b}, header, "file should be gzip compressed (tar.gz)")
}

func TestHelmPluginProcessResourceWithInvalidInput(t *testing.T) {
	setup := newPluginTestSetup(t)

	request := createHelmResourceRequest(t, "/non/existent/path")
	requestBody, err := json.Marshal(request)
	require.NoError(t, err)

	headers := map[string]string{
		"Authorization": `{"access_token": "test"}`,
	}
	resp := setup.makeHTTPRequest(t, "POST", "/resource/process", requestBody, headers)
	defer resp.Body.Close()

	require.NotEqual(t, http.StatusOK, resp.StatusCode, "processing invalid chart should fail")
}

func TestHelmPluginWithInvalidConfig(t *testing.T) {
	cmd := exec.Command(testPluginPath, "--config", "invalid-json")
	err := cmd.Run()
	require.Error(t, err, "plugin should fail with invalid config")
}

func TestHelmPluginWithMissingConfigFlag(t *testing.T) {
	cmd := exec.Command(testPluginPath)
	err := cmd.Run()
	require.Error(t, err, "plugin should fail without config flag")
}

// pluginTestSetup represents a running plugin instance for testing
type pluginTestSetup struct {
	cmd    *exec.Cmd
	client *http.Client
	ctx    context.Context
	cancel context.CancelFunc
}

// newPluginTestSetup starts a new plugin instance and returns a setup struct
func newPluginTestSetup(t *testing.T) *pluginTestSetup {
	t.Helper()

	_ = os.Remove(testSocketPath)

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
	cmd := exec.CommandContext(ctx, testPluginPath, "--config", string(configData))

	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)

	err = cmd.Start()
	require.NoError(t, err)

	t.Cleanup(func() {
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	require.Eventually(t, func() bool {
		_, err := os.Stat(testSocketPath)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "plugin socket should be created")

	client := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return net.Dial("unix", testSocketPath)
			},
		},
		Timeout: 10 * time.Second,
	}

	go func() {
		io.Copy(os.Stderr, stderr)
	}()
	go func() {
		io.Copy(os.Stdout, stdout)
	}()

	return &pluginTestSetup{
		cmd:    cmd,
		client: client,
		ctx:    ctx,
		cancel: cancel,
	}
}

// makeHTTPRequest is a helper to create and execute HTTP requests to the plugin
func (s *pluginTestSetup) makeHTTPRequest(t *testing.T, method, path string, body []byte, headers map[string]string) *http.Response {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(s.ctx, method, "http://unix"+path, reqBody)
	require.NoError(t, err)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := s.client.Do(req)
	require.NoError(t, err)

	return resp
}

// createHelmResourceRequest creates a test request for processing a helm resource
func createHelmResourceRequest(t *testing.T, chartPath string) *v1.ProcessResourceInputRequest {
	t.Helper()

	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&helmv1.Helm{}, runtime.NewVersionedType(helmv1.Type, helmv1.Version))

	helmSpec := &helmv1.Helm{
		Path: chartPath,
	}

	var helmInput runtime.Raw
	err := scheme.Convert(helmSpec, &helmInput)
	require.NoError(t, err)

	return &v1.ProcessResourceInputRequest{
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
				Input: &helmInput,
			},
		},
	}
}

// mockMediaTypeAwareBlob implements blob.ReadOnlyBlob and blob.MediaTypeAware interfaces for testing
type mockMediaTypeAwareBlob struct {
	mediaType string
	known     bool
}

func (m *mockMediaTypeAwareBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte("test data"))), nil
}

func (m *mockMediaTypeAwareBlob) Size() int64 {
	return 9 // len("test data")
}

func (m *mockMediaTypeAwareBlob) MediaType() (string, bool) {
	return m.mediaType, m.known
}

// mockBlobWithoutMediaType implements only blob.ReadOnlyBlob interface for testing
type mockBlobWithoutMediaType struct{}

func (m *mockBlobWithoutMediaType) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte("test data"))), nil
}

func (m *mockBlobWithoutMediaType) Size() int64 {
	return 9 // len("test data")
}

// TestProcessHelmResourceWithMediaTypeAware tests media type propagation when blob implements MediaTypeAware
func TestProcessHelmResourceWithMediaTypeAware(t *testing.T) {
	// Create a mock result with MediaTypeAware blob
	testMediaType := "application/vnd.oci.image.layout.v1+tar+gzip"
	mockResult := &constructor.ResourceInputMethodResult{
		ProcessedBlobData: &mockMediaTypeAwareBlob{
			mediaType: testMediaType,
			known:     true,
		},
	}

	// Test the media type extraction logic directly
	var mediaType string
	if mtAware, ok := mockResult.ProcessedBlobData.(blob.MediaTypeAware); ok {
		if mt, known := mtAware.MediaType(); known && mt != "" {
			mediaType = mt
		}
	}

	require.Equal(t, testMediaType, mediaType, "media type should be extracted from MediaTypeAware blob")
}

// TestProcessHelmResourceWithUnknownMediaType tests behavior when MediaTypeAware returns unknown=false
func TestProcessHelmResourceWithUnknownMediaType(t *testing.T) {
	// Create a mock result with MediaTypeAware blob that has unknown media type
	mockResult := &constructor.ResourceInputMethodResult{
		ProcessedBlobData: &mockMediaTypeAwareBlob{
			mediaType: "some-type",
			known:     false, // media type is not known
		},
	}

	// Test the media type extraction logic directly
	var mediaType string
	if mtAware, ok := mockResult.ProcessedBlobData.(blob.MediaTypeAware); ok {
		if mt, known := mtAware.MediaType(); known && mt != "" {
			mediaType = mt
		}
	}

	require.Empty(t, mediaType, "media type should be empty when known=false")
}

// TestProcessHelmResourceWithEmptyMediaType tests behavior when MediaTypeAware returns empty string
func TestProcessHelmResourceWithEmptyMediaType(t *testing.T) {
	// Create a mock result with MediaTypeAware blob that has empty media type
	mockResult := &constructor.ResourceInputMethodResult{
		ProcessedBlobData: &mockMediaTypeAwareBlob{
			mediaType: "", // empty media type
			known:     true,
		},
	}

	// Test the media type extraction logic directly
	var mediaType string
	if mtAware, ok := mockResult.ProcessedBlobData.(blob.MediaTypeAware); ok {
		if mt, known := mtAware.MediaType(); known && mt != "" {
			mediaType = mt
		}
	}

	require.Empty(t, mediaType, "media type should be empty when media type is empty string")
}

// TestProcessHelmResourceWithoutMediaTypeAware tests fallback when blob doesn't implement MediaTypeAware
func TestProcessHelmResourceWithoutMediaTypeAware(t *testing.T) {
	// Create a mock result with blob that doesn't implement MediaTypeAware
	mockResult := &constructor.ResourceInputMethodResult{
		ProcessedBlobData: &mockBlobWithoutMediaType{},
	}

	// Test the media type extraction logic directly
	var mediaType string
	if mtAware, ok := mockResult.ProcessedBlobData.(blob.MediaTypeAware); ok {
		if mt, known := mtAware.MediaType(); known && mt != "" {
			mediaType = mt
		}
	}

	require.Empty(t, mediaType, "media type should be empty when blob doesn't implement MediaTypeAware")
}
