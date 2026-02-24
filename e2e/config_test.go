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
type: generic.config.ocm.software/v1
configurations:
  - type: registry.provider.ocm.software
    provider:
      type: zot
      version: v1.2.3
  - type: cluster.provider.ocm.software
    provider:
      type: kind
      version: v4.5.6
  - type: cli.provider.ocm.software
    provider:
      type: binary
      path: /usr/local/bin/ocm
`
	err := os.WriteFile(configFilePath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	// Set the flag for configPath
	configPath = configFilePath

	// Parse config
	cfg := ParseConfig()

	// Verify CLI
	cliSpec, ok := cfg.CLI.(*BinaryCLIProviderSpec)
	require.True(t, ok)
	require.Equal(t, "/usr/local/bin/ocm", cliSpec.Path)

	// Verify Registry
	registrySpec, ok := cfg.Registry.(*ZotProviderSpec)
	require.True(t, ok)
	require.Equal(t, "v1.2.3", registrySpec.Version)

	// Verify Cluster
	clusterSpec, ok := cfg.Cluster.(*KindProviderSpec)
	require.True(t, ok)
	require.Equal(t, "v4.5.6", clusterSpec.Version)

	// Test default values when no config is provided
	configPath = "" // reset flag

	cfgDefault := ParseConfig()

	cliDefault, ok := cfgDefault.CLI.(*ImageCLIProviderSpec)
	require.True(t, ok)
	require.Contains(t, cliDefault.Path, "ghcr.io/open-component-model/cli")

	_, ok = cfgDefault.Registry.(*ZotProviderSpec)
	require.True(t, ok)

	_, ok = cfgDefault.Cluster.(*KindProviderSpec)
	require.True(t, ok)
}
