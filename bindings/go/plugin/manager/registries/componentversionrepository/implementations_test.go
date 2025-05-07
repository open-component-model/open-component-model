package componentversionrepository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPing(t *testing.T) {
	// Setup test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create plugin
	plugin := NewComponentVersionRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, server.URL, []byte(`{}`))

	// Test successful ping
	err := plugin.Ping(context.Background())
	assert.NoError(t, err)

	// Test failed ping (by shutting down the server)
	server.Close()
	err = plugin.Ping(context.Background())
	assert.Error(t, err)
}

func TestAddComponentVersion(t *testing.T) {
	// Setup test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+UploadComponentVersion && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create plugin
	plugin := NewComponentVersionRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, server.URL, []byte(`{}`))

	ctx := context.Background()
	err := plugin.AddComponentVersion(ctx, repov1.PostComponentVersionRequest[runtime.Typed]{
		Repository: &v1.OCIRepository{
			BaseUrl: "ocm.software",
		},
		Descriptor: defaultDescriptor(),
	}, map[string]string{})
	assert.NoError(t, err)
}

func TestAddComponentVersionValidationFail(t *testing.T) {
	// Setup test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+UploadComponentVersion && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Setup logger
	repository := &v1.OCIRepository{
		BaseUrl: "ocm.software",
	}
	schemaOCIRegistry, err := jsonschema.Reflect(repository).MarshalJSON()
	require.NoError(t, err)
	// Create plugin
	plugin := NewComponentVersionRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, server.URL, schemaOCIRegistry)

	ctx := context.Background()

	err = plugin.AddComponentVersion(ctx, repov1.PostComponentVersionRequest[runtime.Typed]{
		Repository: repository,
		Descriptor: defaultDescriptor(),
	}, map[string]string{})
	assert.ErrorContains(t, err, "jsonschema validation failed")
}

func TestGetComponentVersion(t *testing.T) {
	// Setup test server
	response := defaultDescriptor()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+DownloadComponentVersion && r.Method == http.MethodGet {
			err := json.NewEncoder(w).Encode(response)
			require.NoError(t, err)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create plugin
	plugin := NewComponentVersionRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, server.URL, []byte(`{}`))

	ctx := context.Background()
	desc, err := plugin.GetComponentVersion(ctx, repov1.GetComponentVersionRequest[runtime.Typed]{
		Repository: &v1.OCIRepository{},
		Name:       "test-plugin",
		Version:    "v1.0.0",
	}, map[string]string{})
	require.NoError(t, err)

	require.Equal(t, response.String(), desc.String())
}

func TestAddLocalResource(t *testing.T) {
	// Setup test server
	desc := defaultDescriptor()
	resource := desc.Component.Resources[0]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+UploadLocalResource && r.Method == http.MethodPost {
			err := json.NewEncoder(w).Encode(resource)
			require.NoError(t, err)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create plugin
	plugin := NewComponentVersionRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, server.URL, []byte(`{}`))

	ctx := context.Background()
	gotResource, err := plugin.AddLocalResource(ctx, repov1.PostLocalResourceRequest[runtime.Typed]{
		Repository: &v1.OCIRepository{},
		Name:       "test-plugin",
		Version:    "v1.0.0",
		Resource:   &resource,
	}, map[string]string{})
	require.NoError(t, err)

	require.Equal(t, resource.String(), gotResource.String())
}

func TestGetLocalResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+DownloadLocalResource && r.Method == http.MethodGet {
			location := r.URL.Query().Get("target_location_value")
			require.NoError(t, os.WriteFile(location, []byte(`test`), os.ModePerm))

			w.WriteHeader(http.StatusOK)

			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create plugin
	plugin := NewComponentVersionRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, server.URL, []byte(`{}`))

	f, err := os.CreateTemp("", "temp_file")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, f.Close())
		require.NoError(t, os.Remove(f.Name()))
	})

	ctx := context.Background()
	err = plugin.GetLocalResource(ctx, repov1.GetLocalResourceRequest[runtime.Typed]{
		Repository: &v1.OCIRepository{},
		Name:       "test-plugin",
		Version:    "v1.0.0",
		TargetLocation: types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        f.Name(),
		},
	}, map[string]string{})
	require.NoError(t, err)

	content, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	require.Equal(t, "test", string(content))
}

func defaultDescriptor() *v2.Descriptor {
	return &v2.Descriptor{
		Component: v2.Component{
			ComponentMeta: v2.ComponentMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
			Provider: "ocm.software",
			Resources: []v2.Resource{
				{
					ElementMeta: v2.ElementMeta{
						ObjectMeta: v2.ObjectMeta{
							Name:    "test-resource",
							Version: "1.0.0",
						},
					},
					SourceRefs: nil,
					Type:       "ociImage",
					Relation:   "local",
					Access: &runtime.Raw{
						Type: runtime.Type{
							Name: "ociArtifact",
						},
						Data: []byte(`{"type":"ociArtifact","imageReference":"test/image:1.0"}`),
					},
					Digest: &v2.Digest{
						HashAlgorithm:          "SHA-256",
						NormalisationAlgorithm: "OciArtifactDigest",
						Value:                  "abcdef1234567890",
					},
					Size: 1024,
				},
			},
		},
	}
}
