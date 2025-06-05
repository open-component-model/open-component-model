package digestprocessor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func TestProcessResourceHandler(t *testing.T) {
	// Setup test server
	response := &v1.ProcessResourceDigestResponse{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ProcessResourceDigest && r.Method == http.MethodPost {
			err := json.NewEncoder(w).Encode(response)
			require.NoError(t, err)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Create plugin
	plugin := NewDigestProcessorPlugin(server.Client(), "test-plugin", server.URL, types.Config{
		ID:         "test-plugin",
		Type:       types.TCP,
		PluginType: types.DigestProcessorPluginType,
	}, server.URL, []byte(`{}`))

	ctx := context.Background()
	_, err := plugin.ProcessResourceDigest(ctx, &v1.ProcessResourceDigestRequest{}, map[string]string{})
	require.NoError(t, err)
}
