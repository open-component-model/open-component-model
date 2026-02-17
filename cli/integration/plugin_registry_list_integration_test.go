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
	ctx := t.Context()

	t.Logf("Starting Integration Test for Plugin OCIRegistry Get Command")

	// Setup environment
	// Two remote registries (A and B) with one plugin registry each
	user := "ocm"
	password := internal.GenerateRandomPassword(t, 20)
	htpasswd := internal.GenerateHtpasswd(t, user, password)

	// OCIRegistry A
	registryURLA := internal.StartDockerContainerRegistry(t, fmt.Sprintf("%s-%d", "registry-a", time.Now().UnixNano()), htpasswd)
	var err error
	oA := internal.ConfigOpts{
		User:     user,
		Password: password,
	}
	oA.Host, oA.Port, err = net.SplitHostPort(registryURLA)
	r.NoError(err)

	// OCIRegistry B
	registryURLB := internal.StartDockerContainerRegistry(t, fmt.Sprintf("%s-%d", "registry-b", time.Now().UnixNano()), htpasswd)
	oB := internal.ConfigOpts{
		User:     user,
		Password: password,
	}
	oB.Host, oB.Port, err = net.SplitHostPort(registryURLB)
	r.NoError(err)

	// Generate and write config for the remote registry
	cfgPath, err := internal.CreateOCMConfigForRegistry(t, []internal.ConfigOpts{oA, oB})
	r.NoError(err)

	// Plugin A
	pluginRegistryComponentA := "ocm.software/plugin-registry-a"
	pluginRegistryVersionA := "v1.0.0"
	pluginRegistryURLA := fmt.Sprintf("http://%s//%s:%s", registryURLA, pluginRegistryComponentA, pluginRegistryVersionA)
	pluginsA := []list.PluginInfo{
		{"plugin-one.io/myplugin", "v1.7.0", []string{"linux/amd64", "window/amd64", "macOS/arm64"}, "Second test plugin", pluginRegistryURLA, ""},
		{"plugin-one.io/myplugin", "v1.4.0", []string{"linux/amd64"}, "First test plugin", pluginRegistryURLA, ""},
		{"plugin-two.io/myplugin", "v1.5.0", []string{"linux/amd64", "windows/adm64", "macOS/arm64"}, "Another test plugin", pluginRegistryURLA, ""},
		{"plugin-two.io/myplugin", "v1.6.0", []string{"linux/amd64", "windows/adm64", "macOS/arm64"}, "Another test plugin", pluginRegistryURLA, ""},
		{"plugin-two.io/myplugin", "v1.4.0", []string{"linux/amd64", "windows/adm64"}, "Another test plugin", pluginRegistryURLA, ""},
	}

	// Create plugin constructors and add them to the registry
	var componentReferencesA string
	for _, plugin := range pluginsA {
		r.NoError(AddComponentForConstructor(ctx, CreatePluginComponentConstructors(plugin.Name, plugin.Version), cfgPath, registryURLA))
		componentReferencesA += GeneratePluginReferences(plugin.Name, plugin.Version, plugin.Description, plugin.Platforms)
	}

	// Create plugin registry constructor and add it to the registry
	r.NoError(AddComponentForConstructor(ctx, CreatePluginRegistryConstructor(pluginRegistryComponentA, pluginRegistryVersionA, componentReferencesA), cfgPath, registryURLA))

	// Plugin B
	pluginRegistryComponentB := "ocm.software/plugin-registry-b"
	pluginRegistryVersionB := "v1.0.0"
	pluginRegistryURLB := fmt.Sprintf("http://%s//%s:%s", registryURLB, pluginRegistryComponentB, pluginRegistryVersionB)
	pluginsB := []list.PluginInfo{
		{"plugin-one.io/myplugin", "v1.4.0", []string{"linux/amd64"}, "First test plugin", pluginRegistryURLB, ""},
		{"plugin-two.io/myplugin", "v1.5.0", []string{"linux/amd64", "windows/adm64", "macOS/arm64"}, "Another test plugin", pluginRegistryURLB, ""},
		{"plugin-two.io/myplugin", "v1.6.0", []string{"linux/amd64", "windows/adm64", "macOS/arm64"}, "Another test plugin", pluginRegistryURLB, ""},
		{"plugin-two.io/myplugin", "v1.4.0", []string{"linux/amd64", "windows/adm64"}, "Another test plugin", pluginRegistryURLB, ""},
	}

	// Create plugin constructors and add them to the registry
	var componentReferencesB string
	for _, plugin := range pluginsB {
		r.NoError(AddComponentForConstructor(ctx, CreatePluginComponentConstructors(plugin.Name, plugin.Version), cfgPath, registryURLB))
		componentReferencesB += GeneratePluginReferences(plugin.Name, plugin.Version, plugin.Description, plugin.Platforms)
	}

	// Create plugin registry constructor and add it to the registry
	r.NoError(AddComponentForConstructor(ctx, CreatePluginRegistryConstructor(pluginRegistryComponentB, pluginRegistryVersionB, componentReferencesB), cfgPath, registryURLB))

	cases := []struct {
		name             string
		pluginRegistries []string
		result           []list.PluginInfo
	}{
		{
			name: "list from one registry",
			pluginRegistries: []string{
				pluginRegistryURLA,
			},
			result: pluginsA,
		},
		{
			name: "list from one registry without registry version",
			pluginRegistries: []string{
				strings.Split(pluginRegistryURLA, ":"+pluginRegistryVersionA)[0],
			},
			result: pluginsA,
		},
		{
			name: "list from both registries",
			pluginRegistries: []string{
				pluginRegistryURLA,
				pluginRegistryURLB,
			},
			result: append(pluginsA, pluginsB...),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var outputBuffer, errorBuffer bytes.Buffer
			getCmd := cmd.New()
			getCmd.SetOut(&outputBuffer)
			getCmd.SetErr(&errorBuffer)

			cmdArgs := []string{
				"plugin",
				"registry",
				"list",
				"--config", cfgPath,
				"--registry", strings.Join(tc.pluginRegistries, ","),
				"--output", "json",
			}

			getCmd.SetArgs(cmdArgs)

			r.NoError(getCmd.ExecuteContext(ctx), "plugin registry list should succeed with a print statement")
			cmdErr := errorBuffer.String()
			if cmdErr != "" {
				t.Logf("Command error output: %s", cmdErr)
			}
			resultJson := outputBuffer.String()

			var actualPlugins []list.PluginInfo
			r.NoError(json.Unmarshal([]byte(resultJson), &actualPlugins), "should be able to unmarshal JSON result")

			SortPluginsByAllFields := func(plugins []list.PluginInfo) {
				slices.SortFunc(plugins, func(a, b list.PluginInfo) int {
					if a.Name != b.Name {
						return strings.Compare(a.Name, b.Name)
					}
					if a.Version != b.Version {
						return strings.Compare(a.Version, b.Version)
					}
					if a.Registry != b.Registry {
						return strings.Compare(a.Registry, b.Registry)
					}
					return 0
				})
			}

			SortPluginsByAllFields(actualPlugins)
			SortPluginsByAllFields(tc.result)

			expected := make([]list.PluginInfo, len(tc.result))
			for i, v := range tc.result {
				v.Name = CreateCompRefNameFromPlugin(v.Name)
				expected[i] = v
			}

			r.Equal(expected, actualPlugins, "plugin registry list should have the same result")
		})
	}
}

func AddComponentForConstructor(ctx context.Context, constructorContent string, cfgPath string, registryURL string) (err error) {
	constructorPath := filepath.Join(os.TempDir(), "constructor.yaml")
	defer os.Remove(constructorPath)

	if err = os.WriteFile(constructorPath, []byte(constructorContent), os.ModePerm); err != nil { //nolint:gosec // test code
		return err
	}

	addCMD := cmd.New()
	addCMD.SetArgs([]string{
		"add",
		"component-version",
		"--repository", fmt.Sprintf("http://%s", registryURL),
		"--constructor", constructorPath,
		"--config", cfgPath,
	})

	return addCMD.ExecuteContext(ctx)
}

func CreatePluginComponentConstructors(name, version string) string {
	return fmt.Sprintf(`
---
name: %s
version: %s
provider:
  name: ocm.software
`, name, version)
}

func CreateCompRefNameFromPlugin(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, ".", "-"), "/", "-")
}

func GeneratePluginReferences(componentName, version, description string, platforms []string) string {
	name := CreateCompRefNameFromPlugin(componentName)

	s := fmt.Sprintf(`
  - name: %s
    version: %s
    componentName: %s
`, name, version, componentName)

	// Add labels if description or platforms are provided
	var labels string
	if description != "" {
		labels += fmt.Sprintf(`
          description: %s
`, description)
	}

	if len(platforms) > 0 {
		labels += `
          platforms:
`

		for _, platform := range platforms {
			labels += fmt.Sprintf(`
            - %s`, strings.TrimSpace(platform))
		}
	}

	if labels != "" {
		s += fmt.Sprintf(`
    labels:
      - name: %s
        value:
%s`, list.PluginInfoKey, labels)
	}

	return s
}

func CreatePluginRegistryConstructor(component, version, references string) string {
	return fmt.Sprintf(`
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
%s
`, component, version, references)
}
