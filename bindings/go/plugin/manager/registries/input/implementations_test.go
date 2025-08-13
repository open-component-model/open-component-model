package input

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func TestProcessResourceHandler(t *testing.T) {
	// Setup test server
	response := &v1.ProcessResourceInputResponse{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ProcessResource && r.Method == http.MethodPost {
			err := json.NewEncoder(w).Encode(response)
			require.NoError(t, err)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create plugin
	plugin := NewConstructionRepositoryPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: types.ComponentVersionRepositoryPluginType,
	}, server.URL, []byte(`{}`))

	ctx := context.Background()
	_, err := plugin.ProcessResource(ctx, &v1.ProcessResourceInputRequest{
		Resource: &v2.Resource{},
	}, map[string]string{})
	require.NoError(t, err)
}
