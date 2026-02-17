package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/cli/cmd"
	"ocm.software/open-component-model/cli/cmd/plugins/list"
	"ocm.software/open-component-model/cli/integration/internal"
)

func Test_Integration_PluginRegistryGet_WithFlag(t *testing.T) {
	r := require.New(t)
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

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
		plugin           string
		version          string
		pluginRegistries []string
		withVersion      bool
		result           []list.PluginInfo
	}{
		{
			name:             "get plugin-one.io/myplugin with version",
			plugin:           "plugin-one.io/myplugin",
			version:          "v1.7.0",
			pluginRegistries: []string{pluginRegistryURLA},
			withVersion:      true,
			result: []list.PluginInfo{
				{"plugin-one.io/myplugin", "v1.7.0", []string{"linux/amd64", "window/amd64", "macOS/arm64"}, "Second test plugin", pluginRegistryURLA, ""},
			},
		},
		{
			name:             "get plugin-one.io/myplugin with version (without registry version)",
			plugin:           "plugin-one.io/myplugin",
			version:          "v1.7.0",
			pluginRegistries: []string{strings.Split(pluginRegistryURLA, ":"+pluginRegistryVersionA)[0]},
			withVersion:      true,
			result: []list.PluginInfo{
				{"plugin-one.io/myplugin", "v1.7.0", []string{"linux/amd64", "window/amd64", "macOS/arm64"}, "Second test plugin", pluginRegistryURLA, ""},
			},
		},
		{
			name:             "get plugin-two.io/myplugin without version",
			plugin:           "plugin-two.io/myplugin",
			pluginRegistries: []string{pluginRegistryURLA},
			withVersion:      false,
			result: []list.PluginInfo{
				{"plugin-two.io/myplugin", "v1.5.0", []string{"linux/amd64", "windows/adm64", "macOS/arm64"}, "Another test plugin", pluginRegistryURLA, ""},
				{"plugin-two.io/myplugin", "v1.6.0", []string{"linux/amd64", "windows/adm64", "macOS/arm64"}, "Another test plugin", pluginRegistryURLA, ""},
				{"plugin-two.io/myplugin", "v1.4.0", []string{"linux/amd64", "windows/adm64"}, "Another test plugin", pluginRegistryURLA, ""},
			},
		},
		{
			name:             "get plugin-one.io/myplugin from two remote plugin registries",
			plugin:           "plugin-one.io/myplugin",
			pluginRegistries: []string{pluginRegistryURLA, pluginRegistryURLB},
			withVersion:      false,
			result: []list.PluginInfo{
				{"plugin-one.io/myplugin", "v1.7.0", []string{"linux/amd64", "window/amd64", "macOS/arm64"}, "Second test plugin", pluginRegistryURLA, ""},
				{"plugin-one.io/myplugin", "v1.4.0", []string{"linux/amd64"}, "First test plugin", pluginRegistryURLA, ""},
				{"plugin-one.io/myplugin", "v1.4.0", []string{"linux/amd64"}, "First test plugin", pluginRegistryURLB, ""},
			},
		},
		{
			name:             "missing plugin",
			plugin:           "non-existent-plugin",
			pluginRegistries: []string{pluginRegistryURLA, pluginRegistryURLB},
			withVersion:      false,
			result:           []list.PluginInfo{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var outputBuffer, errorBuffer bytes.Buffer
			getCmd := cmd.New()
			getCmd.SetOut(&outputBuffer)
			getCmd.SetErr(&errorBuffer)

			name := CreateCompRefNameFromPlugin(tc.plugin)

			cmdArgs := []string{
				"plugin",
				"registry",
				"get",
				name,
				"--config", cfgPath,
				"--registry", strings.Join(tc.pluginRegistries, ","),
				"--output", "json",
			}

			if tc.withVersion {
				cmdArgs = append(cmdArgs, "--version", tc.version)
			}

			getCmd.SetArgs(cmdArgs)

			err := getCmd.ExecuteContext(ctx)
			if err != nil && len(tc.result) != 0 {
				t.Fatal("plugin registry get should succeed with a print statement")
			}

			cmdErr := errorBuffer.String()
			if len(tc.result) == 0 {
				r.Contains(
					cmdErr,
					fmt.Sprintf("Error: plugin %q not found in specified registries: %q\n", tc.plugin, strings.Join(tc.pluginRegistries, ", ")),
					"should return specified error message")
				return
			}

			if cmdErr != "" {
				t.Logf("Command error output: %s", cmdErr)
			}
			resultJson := outputBuffer.String()

			var actualPlugins []list.PluginInfo
			r.NoError(json.Unmarshal([]byte(resultJson), &actualPlugins), "should be able to unmarshal JSON result")

			sortPluginsByAllFields := func(plugins []list.PluginInfo) {
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

			sortPluginsByAllFields(actualPlugins)
			sortPluginsByAllFields(tc.result)

			expected := make([]list.PluginInfo, len(tc.result))
			for i, v := range tc.result {
				v.Name = name
				expected[i] = v
			}

			r.Equal(expected, actualPlugins, "plugin registry get should have the same result")
		})
	}
}
