package componentversionrepository

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
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

	// Setup logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Create plugin
	plugin := NewComponentVersionRepositoryPlugin(
		context.Background(),
		logger,
		server.Client(),
		"test-plugin",
		server.URL,
		types.Config{
			ID:         "test-plugin",
			Type:       types.TCP,
			PluginType: types.ComponentVersionRepositoryPluginType,
			Location:   server.URL,
		},
		[]byte(`{}`), // Empty schema for simplicity
	)

	// Test successful ping
	err := plugin.Ping(context.Background())
	assert.NoError(t, err)

	// Test failed ping (by shutting down the server)
	server.Close()
	err = plugin.Ping(context.Background())
	assert.Error(t, err)
}
