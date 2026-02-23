package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseConfig(t *testing.T) {
	// Create a temp config file
	tempDir := t.TempDir()
	configFilePath := filepath.Join(tempDir, "config.yaml")

	yamlContent := `
ocm:
  source: "binary"
  path: "/usr/local/bin/ocm"
registry:
  provider: "my-registry"
  version: "v1.2.3"
cluster:
  provider: "my-cluster"
  version: "v4.5.6"
`
	err := os.WriteFile(configFilePath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	// Set the flag for configPath
	configPath = configFilePath

	// Parse config
	cfg := ParseConfig()

	// Verify
	require.Equal(t, "binary", cfg.OCM.Source)
	require.Equal(t, "/usr/local/bin/ocm", cfg.OCM.Path)
	require.Equal(t, "my-registry", cfg.Registry.Provider)
	require.Equal(t, "v1.2.3", cfg.Registry.Version)
	require.Equal(t, "my-cluster", cfg.Cluster.Provider)
	require.Equal(t, "v4.5.6", cfg.Cluster.Version)

	// Test default values when no config is provided
	configPath = "" // reset flag

	cfgDefault := ParseConfig()
	require.Equal(t, "image", cfgDefault.OCM.Source)
	require.Equal(t, "zot", cfgDefault.Registry.Provider)
	require.Equal(t, "kind", cfgDefault.Cluster.Provider)
}
