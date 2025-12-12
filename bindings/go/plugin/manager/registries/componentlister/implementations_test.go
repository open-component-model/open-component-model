package componentlister

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/componentlister/v1"
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
	plugin := NewComponentListerPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.ComponentListerPluginType,
	}, server.URL, dummyCapability([]byte(`{}`)))

	// Test successful ping
	err := plugin.Ping(context.Background())
	assert.NoError(t, err)

	// Test failed ping (by shutting down the server)
	server.Close()
	err = plugin.Ping(context.Background())
	assert.Error(t, err)
}

func TestListComponentsHandler(t *testing.T) {
	// Setup test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ListComponents && r.Method == http.MethodPost {
			serverResponse := v1.ListComponentsResponse{List: []string{"test-component-1", "test-component-2"}}
			err := json.NewEncoder(w).Encode(serverResponse)
			require.NoError(t, err)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create plugin
	plugin := NewComponentListerPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: v1.ComponentListerPluginType,
	}, server.URL, dummyCapability([]byte(`{}`)))

	ctx := context.Background()
	response, err := plugin.ListComponents(ctx, &v1.ListComponentsRequest[runtime.Typed]{
		Repository: &runtime.Raw{Type: dummyType, Data: []byte(`{}`)},
		Last:       "",
	}, map[string]string{})
	require.NoError(t, err)
	require.Equal(t, []string{"test-component-1", "test-component-2"}, response.List)
}
