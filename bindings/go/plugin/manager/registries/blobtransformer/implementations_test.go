package blobtransformer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestPing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	plugin := NewPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.BlobTransformerPluginType,
	}, server.URL, dummyCapability([]byte(`{}`)))

	err := plugin.Ping(context.Background())
	assert.NoError(t, err)

	server.Close()
	err = plugin.Ping(context.Background())
	assert.Error(t, err)
}

func TestTransformBlob(t *testing.T) {
	type repoSchema struct {
		Type    string `json:"type"`
		BaseUrl string `json:"baseUrl"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == TransformBlob && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(&v1.TransformBlobResponse{
				Location: types.Location{
					LocationType: types.LocationTypeLocalFile,
					Value:        "/dummy/local-file",
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	schema, err := jsonschema.Reflect(&repoSchema{}).MarshalJSON()
	require.NoError(t, err)
	plugin := NewPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.BlobTransformerPluginType,
	}, server.URL, dummyCapability(schema))

	req := &v1.TransformBlobRequest[runtime.Typed]{
		Specification: &dummyv1.Repository{
			Type:    dummyType,
			BaseUrl: "ocm.software",
		},
	}
	resp, err := plugin.TransformBlob(context.Background(), req, map[string]string{"token": "abc"})
	require.NoError(t, err)
	require.Equal(t, types.LocationTypeLocalFile, resp.Location.LocationType)
	require.Equal(t, "/dummy/local-file", resp.Location.Value)
}

func TestTransformBlobValidationFail(t *testing.T) {
	schema, err := jsonschema.Reflect(&dummyv1.Repository{}).MarshalJSON()
	require.NoError(t, err)
	plugin := NewPlugin(http.DefaultClient, "test-plugin", "", types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.BlobTransformerPluginType,
	}, "", dummyCapability(schema))

	req := &v1.TransformBlobRequest[runtime.Typed]{
		Specification: &dummyv1.Repository{
			Type: dummyType,
		}, // missing required fields
	}
	_, err = plugin.TransformBlob(context.Background(), req, map[string]string{})
	assert.ErrorContains(t, err, "validation")
}

func TestGetIdentity(t *testing.T) {
	type repoSchema struct {
		Type    string `json:"type"`
		BaseUrl string `json:"baseUrl"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == Identity && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(&v1.GetIdentityResponse{Identity: map[string]string{"id": "mock"}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	schema, err := jsonschema.Reflect(&repoSchema{}).MarshalJSON()
	require.NoError(t, err)
	plugin := NewPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.BlobTransformerPluginType,
	}, server.URL, dummyCapability(schema))

	req := &v1.GetIdentityRequest[runtime.Typed]{
		Typ: &dummyv1.Repository{
			Type:    runtime.NewVersionedType("DummyRepository", "v1"),
			BaseUrl: "ocm.software",
		},
	}
	resp, err := plugin.GetIdentity(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"id": "mock"}, resp.Identity)
}

func TestGetIdentityValidationFail(t *testing.T) {
	schema, err := jsonschema.Reflect(&dummyv1.Repository{}).MarshalJSON()
	require.NoError(t, err)
	plugin := NewPlugin(http.DefaultClient, "test-plugin", "", types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.BlobTransformerPluginType,
	}, "", dummyCapability(schema))

	req := &v1.GetIdentityRequest[runtime.Typed]{
		Typ: &dummyv1.Repository{
			Type: dummyType,
		}, // missing required fields
	}
	_, err = plugin.GetIdentity(context.Background(), req)
	assert.ErrorContains(t, err, "validation")
}
