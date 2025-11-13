package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/cmd/plugins/list"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_PluginRegistryList_WithFlag(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	t.Logf("Starting Integration Test for Plugin Registry List Command")
	// Setup credentials and htpasswd
	user := "ocm"
	password := internal.GenerateRandomPassword(t, 20)
	htpasswd := internal.GenerateHtpasswd(t, user, password)

	testFilePath := filepath.Join(t.TempDir(), "test-file.txt")
	r.NoError(os.WriteFile(testFilePath, []byte("foobar"), 0o600), "could not create test file")

	cases := []struct {
		name             string
		pluginRegistries []string
		plugins          map[string][]list.PluginInfo
	}{
		{
			name:             "one remote, one plugin registry",
			pluginRegistries: []string{"registry-one"},
			plugins: map[string][]list.PluginInfo{
				"remote-registry": {
					{"plugin-one", "v1.7.0", "linux", "amd64", "Second test plugin", "registry-one"},
					{"plugin-one", "v1.7.0", "windows", "amd64", "Second test plugin", "registry-one"},
					{"plugin-one", "v1.7.0", "macOs", "arm64", "Second test plugin", "registry-one"},
					{"plugin-one", "v1.4.0", "linux", "amd64", "First test plugin", "registry-one"},
					{"plugin-two", "v1.5.0", "linux", "amd64", "Another test plugin", "registry-one"},
					{"plugin-two", "v1.5.0", "windows", "amd64", "Another test plugin", "registry-one"},
					{"plugin-two", "v1.5.0", "macOs", "ard64", "Another test plugin", "registry-one"},
				},
			},
		},
		{
			name:             "one remote, two plugin registries",
			pluginRegistries: []string{"registry-one", "registry-two"},
			plugins: map[string][]list.PluginInfo{
				"remote-registry": {
					{"plugin-one", "v1.7.0", "linux", "amd64", "Second test plugin", "registry-one"},
					{"plugin-one", "v1.7.0", "windows", "amd64", "Second test plugin", "registry-one"},
					{"plugin-one", "v1.7.0", "macOs", "arm64", "Second test plugin", "registry-one"},
					{"plugin-one", "v1.4.0", "linux", "amd64", "First test plugin", "registry-one"},
					{"plugin-two", "v1.5.0", "linux", "amd64", "First test plugin", "registry-two"},
					{"plugin-two", "v1.5.0", "windows", "amd64", "First test plugin", "registry-two"},
					{"plugin-two", "v1.5.0", "macOs", "ard64", "First test plugin", "registry-two"},
				},
			},
		},
		{
			name:             "two remotes, two plugin registries",
			pluginRegistries: []string{"registry-one", "registry-two"},
			plugins: map[string][]list.PluginInfo{
				"remote-registry-1": {
					{"plugin-one", "v1.7.0", "linux", "amd64", "What a test", "registry-one"},
					{"plugin-one", "v1.7.0", "linux", "arm64", "Nice", "registry-one"},
					{"plugin-one", "v1.7.0", "windows", "amd64", "First test plugin", "registry-one"},
				},
				"remote-registry-2": {
					{"plugin-two", "v1.7.0", "windows", "amd64", "Hello World", "registry-two"},
					{"plugin-two", "v1.7.0", "linux", "amd64", "Hello OCM", "registry-two"},
					{"plugin-three", "v1.0.0", "windows", "amd64", "Hello Plugin", "registry-two"},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
			defer cancel()

			var pluginRegistryAddresses []string

			cfgPath := filepath.Join(t.TempDir(), "ocmconfig.yaml")
			cfg := fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers:
`)

			for remote, pluginList := range tc.plugins {
				// Start a registry for each remote registry
				t.Logf("Start remote registries and prepare config")
				containerName := fmt.Sprintf("%s-%d", remote, time.Now().UnixNano())
				registryAddress := internal.StartDockerContainerRegistry(t, containerName, htpasswd)
				host, port, err := net.SplitHostPort(registryAddress)
				r.NoError(err)

				// Generate and write config for the remote registry
				cfg += fmt.Sprintf(`
  - identity:
      type: OCIRepository
      hostname: %q
      port: %q
      scheme: http
    credentials:
    - type: Credentials/v1
      properties:
        username: %q
        password: %q
`, host, port, user, password)
				r.NoError(os.WriteFile(cfgPath, []byte(cfg), os.ModePerm))

				t.Logf("Generated config:\n%s", cfg)

				// Create constructor file for each plugin and add them to their remote registries
				var versions []string
				for _, plugin := range pluginList {
					// Skip if version already exists
					if !slices.Contains(versions, plugin.Version) {
						r.NoError(addComponent(ctx, createPluginComponentConstructors(plugin), cfgPath, registryAddress))
						versions = append(versions, plugin.Version)
					}
				}

				var pluginRegistryAddress string

				// Create constructor file for each plugin registry containing the plugin references
				for _, pluginRegistry := range tc.pluginRegistries {
					componentName := fmt.Sprintf("ocm.software/%s", pluginRegistry)
					componentVersion := "v1.0.0"

					// Create constructor file for component containing the plugin registry
					constructorPluginRegistry := fmt.Sprintf(`
name: %s
version: %s 
provider:
  name: ocm.software
labels:
  - name: category
    value: plugin-registry
  - name: registry
    value: official
  - name: description
    value: Official OCM plugin registry

componentReferences:
`, componentName, componentVersion)

					for _, plugin := range pluginList {
						if plugin.Registry == pluginRegistry {
							constructorPluginRegistry += generatePluginReferences(plugin)
							pluginRegistryAddress = fmt.Sprintf("http://%s//%s:%s", registryAddress, componentName, componentVersion)
							if !slices.Contains(pluginRegistryAddresses, pluginRegistryAddress) {
								pluginRegistryAddresses = append(pluginRegistryAddresses, pluginRegistryAddress)
							}
						}
					}

					r.NoError(addComponent(ctx, constructorPluginRegistry, cfgPath, registryAddress))
				}

			}

			var outputBuffer bytes.Buffer
			listCMDJSON := cmd.New()
			listCMDJSON.SetOut(&outputBuffer)
			listCMDJSON.SetArgs([]string{
				"plugin",
				"registry",
				"list",
				"--config", cfgPath,
				"--registry", strings.Join(pluginRegistryAddresses, ","),
				"--output", "json",
			})

			r.NoError(listCMDJSON.ExecuteContext(ctx), "plugin registry list should succeed with a print statement")
			resultJson := outputBuffer.String()

			var actualPlugins []list.PluginInfo
			r.NoError(json.Unmarshal([]byte(resultJson), &actualPlugins), "should be able to unmarshal JSON result")

			var expectedPlugins []list.PluginInfo
			for _, pluginList := range tc.plugins {
				for _, plugin := range pluginList {
					// Update registry field with actual registry address
					expectedPlugin := plugin
					// Find matching registry address for this plugin's registry name
					for _, addr := range pluginRegistryAddresses {
						if strings.Contains(addr, plugin.Registry) {
							expectedPlugin.Registry = addr
							break
						}
					}
					expectedPlugins = append(expectedPlugins, expectedPlugin)
				}
			}

			// Compare the results
			r.Len(actualPlugins, len(expectedPlugins), "should have correct number of plugins")

			// Resulting plugins were sorted
			slices.SortFunc(actualPlugins, func(a, b list.PluginInfo) int {
				if a.Name != b.Name {
					return strings.Compare(a.Name, b.Name)
				}
				return strings.Compare(a.Version, b.Version)
			})
			slices.SortFunc(expectedPlugins, func(a, b list.PluginInfo) int {
				if a.Name != b.Name {
					return strings.Compare(a.Name, b.Name)
				}
				return strings.Compare(a.Version, b.Version)
			})

			// Compare each plugin
			for i, expected := range expectedPlugins {
				r.Equal(expected.Name, actualPlugins[i].Name, "plugin name should match")
				r.Equal(expected.Version, actualPlugins[i].Version, "plugin version should match")
				r.Equal(expected.Os, actualPlugins[i].Os, "plugin OS should match")
				r.Equal(expected.Arch, actualPlugins[i].Arch, "plugin architecture should match")
				r.Equal(expected.Description, actualPlugins[i].Description, "plugin description should match")
				r.Equal(expected.Registry, actualPlugins[i].Registry, "plugin registry should match")
			}
		})
	}
}

func addComponent(ctx context.Context, constructorContent string, cfgPath string, registryAddress string) (err error) {
	constructorPath := filepath.Join(os.TempDir(), "constructor.yaml")
	defer func(name string) {
		err = os.Remove(name)
	}(constructorPath)

	if err = os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm); err != nil {
		return err
	}

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("http://%s", registryAddress),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})

	return addCMD.ExecuteContext(ctx)
}

func createPluginComponentConstructors(info list.PluginInfo) string {
	var s string
	s += fmt.Sprintf(`
---
name: %s
version: %s 
provider:
  name: ocm.software
`, info.Name, info.Version)

	return s
}

func generatePluginReferences(plugin list.PluginInfo) string {
	var s string

	s += fmt.Sprintf(`
  - name: %s
    version: %s
    componentName: %s
`, plugin.Name, plugin.Version, plugin.Name)

	var labels string
	if plugin.Description != "" {
		labels += fmt.Sprintf(`
    - name: description
      value: %s
`, plugin.Description)
	}

	if plugin.Os != "" {
		labels += fmt.Sprintf(`
    - name: os
      value: %s
`, plugin.Os)
	}

	if plugin.Arch != "" {
		labels += fmt.Sprintf(`
    - name: arch
      value: %s
`, plugin.Arch)

	}

	if labels != "" {
		s += fmt.Sprintf(`    labels:%s`, labels)
	}

	return s
}
