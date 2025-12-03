package resource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetGlobalResource(t *testing.T) {
	// Setup test server
	response := &v1.GetGlobalResourceRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == GetGlobalResource && r.Method == http.MethodPost {
			err := json.NewEncoder(w).Encode(response)
			require.NoError(t, err)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create plugin
	plugin := NewResourceRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.ResourceRepositoryPluginType,
	}, server.URL, dummyCapability([]byte(`{}`)))

	ctx := context.Background()
	_, err := plugin.GetGlobalResource(ctx, &v1.GetGlobalResourceRequest{
		Resource: &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "test-resource",
					Version: "1.0.0",
				},
			},
			Type:     "test",
			Relation: descriptorv2.LocalRelation,
			Access: &runtime.Raw{
				Type: dummyType,
				Data: []byte(`{ "foo": "bar" }`),
			},
		},
	}, map[string]string{})
	require.NoError(t, err)
}

func TestAddGlobalResource(t *testing.T) {
	// Setup test server
	response := &v1.GetGlobalResourceResponse{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == AddGlobalResource && r.Method == http.MethodPost {
			err := json.NewEncoder(w).Encode(response)
			require.NoError(t, err)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create plugin
	plugin := NewResourceRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.ResourceRepositoryPluginType,
	}, server.URL, dummyCapability([]byte(`{}`)))

	ctx := context.Background()
	_, err := plugin.AddGlobalResource(ctx, &v1.AddGlobalResourceRequest{
		Resource: &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "test-resource",
					Version: "1.0.0",
				},
			},
			Type:     "test",
			Relation: descriptorv2.LocalRelation,
			Access: &runtime.Raw{
				Type: dummyType,
				Data: []byte(`{ "foo": "bar" }`),
			},
		},
	}, map[string]string{})
	require.NoError(t, err)
}

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
	plugin := NewResourceRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.ResourceRepositoryPluginType,
	}, server.URL, v1.CapabilitySpec{})

	ctx := context.Background()
	err := plugin.Ping(ctx)
	require.NoError(t, err)
}

func TestValidateEndpoint(t *testing.T) {
	// Setup test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create plugin with valid schema
	validSchema := `{
		"type": "object",
		"properties": {
			"type": {
				"type": "string"
			}
		},
		"required": ["type"]
	}`
	plugin := NewResourceRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.ResourceRepositoryPluginType,
	}, server.URL, dummyCapability([]byte(validSchema)))

	// Test valid object
	validObj := &runtime.Raw{
		Type: dummyType,
		Data: []byte(`{ "type": "test" }`),
	}
	err := plugin.validateEndpoint(validObj)
	require.NoError(t, err)

	// Test invalid object
	invalidObj := &runtime.Raw{
		Type: runtime.Type{
			Name:    "",
			Version: "v1",
		},
	}
	err = plugin.validateEndpoint(invalidObj)
	require.Error(t, err)
}
