package input

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
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
		PluginType: v1.InputPluginType,
	}, server.URL, dummyCapability([]byte(`{}`)))

	ctx := context.Background()
	_, err := plugin.ProcessResource(ctx, &v1.ProcessResourceInputRequest{
		Resource: &constructorv1.Resource{
			AccessOrInput: constructorv1.AccessOrInput{
				Input: &runtime.Raw{
					Type: dummyType,
					Data: []byte(`{}`),
				},
			},
		},
	}, map[string]string{})
	require.NoError(t, err)
}
