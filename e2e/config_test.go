package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	genericspecv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
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
	cliConfigs, err := genericspecv1.FilterForType[*CLIProviderConfig](DefaultScheme, cfg)
	require.NoError(t, err)
	require.Len(t, cliConfigs, 1)
	cliProvider, err := DecodeProvider(cliConfigs[0].Provider)
	require.NoError(t, err)
	cliSpec, ok := cliProvider.(*BinaryCLIProviderSpec)
	require.True(t, ok)
	require.Equal(t, "/usr/local/bin/ocm", cliSpec.Path)

	// Verify Registry
	registryConfigs, err := genericspecv1.FilterForType[*RegistryProviderConfig](DefaultScheme, cfg)
	require.NoError(t, err)
	require.Len(t, registryConfigs, 1)
	registryProvider, err := DecodeProvider(registryConfigs[0].Provider)
	require.NoError(t, err)
	registrySpec, ok := registryProvider.(*ZotProviderSpec)
	require.True(t, ok)
	require.Equal(t, "v1.2.3", registrySpec.Version)

	// Verify Cluster
	clusterConfigs, err := genericspecv1.FilterForType[*ClusterProviderConfig](DefaultScheme, cfg)
	require.NoError(t, err)
	require.Len(t, clusterConfigs, 1)
	clusterProvider, err := DecodeProvider(clusterConfigs[0].Provider)
	require.NoError(t, err)
	clusterSpec, ok := clusterProvider.(*KindProviderSpec)
	require.True(t, ok)
	require.Equal(t, "v4.5.6", clusterSpec.Version)

	// Test default values when no config is provided
	configPath = "" // reset flag

	cfgDefault := ParseConfig()
	require.NotNil(t, cfgDefault)
	require.Empty(t, cfgDefault.Configurations) // the defaults are handled in framework_test.go now, ParseConfig simply parses whatever is there.
}
