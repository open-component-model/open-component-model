package plugins_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/cli/cmd/internal/test"
	"ocm.software/open-component-model/bindings/go/cli/cmd/plugins/list"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
)

const (
	registryComponent = "ocm.software/plugin-registry"
	registryVersion   = "1.0.0"
)

// setupPluginRegistry creates a CTF containing a registry component version
// whose References point to plugin components with ocm.software/pluginInfo labels.
// It returns the archive path suitable for building a compref.Ref.
func setupPluginRegistry(t *testing.T, desc *descriptor.Descriptor) string {
	t.Helper()
	r := require.New(t)

	archivePath := t.TempDir()
	fs, err := filesystem.NewFS(archivePath, os.O_RDWR)
	r.NoError(err)
	archive := ctf.NewFileSystemCTF(fs)
	repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
	r.NoError(err)

	r.NoError(repo.AddComponentVersion(t.Context(), desc))
	return archivePath
}

// pluginInfoLabel creates a label with the ocm.software/pluginInfo key.
func pluginInfoLabel(t *testing.T, description string, platforms []string) descriptor.Label {
	t.Helper()
	info := struct {
		Description string   `json:"description"`
		Platforms   []string `json:"platforms"`
	}{
		Description: description,
		Platforms:   platforms,
	}
	raw, err := json.Marshal(info)
	require.NoError(t, err)
	return descriptor.Label{
		Name:  list.PluginInfoKey,
		Value: raw,
	}
}

// newRegistryDescriptor builds a registry component descriptor with the given references.
func newRegistryDescriptor(name, version string, refs []descriptor.Reference) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
			Provider:   descriptor.Provider{Name: "ocm.software"},
			References: refs,
		},
	}
}

// registryRef returns the string reference that the --registry flag accepts.
func registryRef(archivePath, component, version string) string {
	ref := compref.Ref{
		Repository: &ctfv1.Repository{FilePath: archivePath},
		Component:  component,
		Version:    version,
	}
	return ref.String()
}

// registryRefNoVersion returns a reference without a version so the command
// resolves the latest version automatically.
func registryRefNoVersion(archivePath, component string) string {
	ref := compref.Ref{
		Repository: &ctfv1.Repository{FilePath: archivePath},
		Component:  component,
	}
	return ref.String()
}

func TestPluginRegistryList(t *testing.T) {
	refs := []descriptor.Reference{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "my-plugin",
					Version: "0.1.0",
					Labels: []descriptor.Label{
						pluginInfoLabel(t, "A test plugin", []string{"linux/amd64", "darwin/arm64"}),
					},
				},
			},
			Component: "ocm.software/plugins/my-plugin",
		},
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "another-plugin",
					Version: "2.0.0",
					Labels: []descriptor.Label{
						pluginInfoLabel(t, "Another plugin", []string{"linux/amd64"}),
					},
				},
			},
			Component: "ocm.software/plugins/another-plugin",
		},
	}

	desc := newRegistryDescriptor(registryComponent, registryVersion, refs)
	archivePath := setupPluginRegistry(t, desc)
	regRef := registryRef(archivePath, registryComponent, registryVersion)

	t.Run("json output contains all plugins", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "list", "--registry", regRef, "-o", "json"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		var plugins []list.PluginInfo
		r.NoError(json.Unmarshal(out.Bytes(), &plugins))
		r.Len(plugins, 2)

		// Sorted by name: another-plugin < my-plugin
		r.Equal("another-plugin", plugins[0].Name)
		r.Equal("2.0.0", plugins[0].Version)
		r.Equal("Another plugin", plugins[0].Description)
		r.Equal([]string{"linux/amd64"}, plugins[0].Platforms)

		r.Equal("my-plugin", plugins[1].Name)
		r.Equal("0.1.0", plugins[1].Version)
		r.Equal("A test plugin", plugins[1].Description)
		r.Equal([]string{"linux/amd64", "darwin/arm64"}, plugins[1].Platforms)
	})

	t.Run("yaml output contains all plugins", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "list", "--registry", regRef, "-o", "yaml"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		result := out.String()
		r.Contains(result, "another-plugin")
		r.Contains(result, "my-plugin")
		r.Contains(result, "A test plugin")
		r.Contains(result, "Another plugin")
	})

	t.Run("table output contains plugin names and versions", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "list", "--registry", regRef, "-o", "table"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		result := out.String()
		r.Contains(result, "another-plugin")
		r.Contains(result, "my-plugin")
		r.Contains(result, "0.1.0")
		r.Contains(result, "2.0.0")
		// Table header (go-pretty renders headers in uppercase)
		r.Contains(result, "NAME")
		r.Contains(result, "VERSION")
		r.Contains(result, "DESCRIPTION")
	})

	t.Run("wide output includes registry column", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "list", "--registry", regRef, "-o", "wide"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		result := out.String()

		r.Contains(result, "REGISTRY")
		r.Contains(result, "another-plugin")
		r.Contains(result, "my-plugin")
	})

	t.Run("ndjson output has one JSON object per line", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "list", "--registry", regRef, "-o", "ndjson"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		// NDJSON: each line is a valid JSON plugin object
		dec := json.NewDecoder(out)
		var plugins []list.PluginInfo
		for dec.More() {
			var p list.PluginInfo
			r.NoError(dec.Decode(&p))
			plugins = append(plugins, p)
		}
		r.Len(plugins, 2)
	})

	t.Run("latest version resolution when version omitted", func(t *testing.T) {
		r := require.New(t)

		// Create a second version to ensure latest is picked
		refsV2 := []descriptor.Reference{
			{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "my-plugin",
						Version: "3.0.0",
						Labels: []descriptor.Label{
							pluginInfoLabel(t, "Updated plugin", []string{"linux/amd64"}),
						},
					},
				},
				Component: "ocm.software/plugins/my-plugin",
			},
		}
		descV2 := newRegistryDescriptor(registryComponent, "2.0.0", refsV2)

		// Use separate CTF containing both versions
		multiArchivePath := t.TempDir()
		fs, err := filesystem.NewFS(multiArchivePath, os.O_RDWR)
		r.NoError(err)
		archive := ctf.NewFileSystemCTF(fs)
		repo, err := oci.NewRepository(ocictf.WithCTF(ocictf.NewFromCTF(archive)))
		r.NoError(err)

		r.NoError(repo.AddComponentVersion(t.Context(), desc))
		r.NoError(repo.AddComponentVersion(t.Context(), descV2))

		noVersionRef := registryRefNoVersion(multiArchivePath, registryComponent)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err = test.OCM(t,
			test.WithArgs("plugin", "registry", "list", "--registry", noVersionRef, "-o", "json"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		var plugins []list.PluginInfo
		r.NoError(json.Unmarshal(out.Bytes(), &plugins))
		// Should show plugins from the latest version (2.0.0)
		r.Len(plugins, 1)
		r.Equal("my-plugin", plugins[0].Name)
		r.Equal("3.0.0", plugins[0].Version)
		r.Equal("Updated plugin", plugins[0].Description)
	})

	t.Run("invalid registry reference returns error", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "list", "--registry", ":::invalid", "-o", "json"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.Error(err)
	})

	t.Run("empty registry with no plugins produces empty list", func(t *testing.T) {
		r := require.New(t)
		emptyDesc := newRegistryDescriptor(registryComponent, "0.0.1", nil)
		emptyPath := setupPluginRegistry(t, emptyDesc)
		emptyRef := registryRef(emptyPath, registryComponent, "0.0.1")

		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "list", "--registry", emptyRef, "-o", "json"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		var plugins []list.PluginInfo
		r.NoError(json.Unmarshal(out.Bytes(), &plugins))
		r.Empty(plugins)
	})
}

func TestPluginRegistryGet(t *testing.T) {
	refs := []descriptor.Reference{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "my-plugin",
					Version: "0.1.0",
					Labels: []descriptor.Label{
						pluginInfoLabel(t, "A test plugin", []string{"linux/amd64", "darwin/arm64"}),
					},
				},
			},
			Component: "ocm.software/plugins/my-plugin",
		},
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "my-plugin",
					Version: "0.2.0",
					Labels: []descriptor.Label{
						pluginInfoLabel(t, "A test plugin v2", []string{"linux/amd64"}),
					},
				},
			},
			Component: "ocm.software/plugins/my-plugin",
		},
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "another-plugin",
					Version: "2.0.0",
					Labels: []descriptor.Label{
						pluginInfoLabel(t, "Another plugin", []string{"linux/amd64"}),
					},
				},
			},
			Component: "ocm.software/plugins/another-plugin",
		},
	}

	desc := newRegistryDescriptor(registryComponent, registryVersion, refs)
	archivePath := setupPluginRegistry(t, desc)
	regRef := registryRef(archivePath, registryComponent, registryVersion)

	t.Run("get plugin by name returns all versions", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "get", "my-plugin", "--registry", regRef, "-o", "json"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		var plugins []list.PluginInfo
		r.NoError(json.Unmarshal(out.Bytes(), &plugins))
		r.Len(plugins, 2)
		r.Equal("my-plugin", plugins[0].Name)
		r.Equal("my-plugin", plugins[1].Name)
	})

	t.Run("get plugin with specific version", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "get", "my-plugin", "--registry", regRef, "--version", "0.2.0", "-o", "json"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		var plugins []list.PluginInfo
		r.NoError(json.Unmarshal(out.Bytes(), &plugins))
		r.Len(plugins, 1)
		r.Equal("my-plugin", plugins[0].Name)
		r.Equal("0.2.0", plugins[0].Version)
		r.Equal("A test plugin v2", plugins[0].Description)
	})

	t.Run("get plugin with inline version syntax", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "get", "my-plugin:0.1.0", "--registry", regRef, "-o", "json"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		var plugins []list.PluginInfo
		r.NoError(json.Unmarshal(out.Bytes(), &plugins))
		r.Len(plugins, 1)
		r.Equal("my-plugin", plugins[0].Name)
		r.Equal("0.1.0", plugins[0].Version)
	})

	t.Run("get plugin table output", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "get", "another-plugin", "--registry", regRef, "-o", "table"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		result := out.String()
		r.Contains(result, "another-plugin")
		r.Contains(result, "2.0.0")
		r.Contains(result, "Another plugin")
	})

	t.Run("get nonexistent plugin returns error", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "get", "nonexistent", "--registry", regRef, "-o", "json"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.Error(err)
		r.ErrorContains(err, `plugin "nonexistent" not found`)
	})

	t.Run("get yaml output", func(t *testing.T) {
		r := require.New(t)
		out := new(bytes.Buffer)
		logs := test.NewJSONLogReader()
		_, err := test.OCM(t,
			test.WithArgs("plugin", "registry", "get", "another-plugin", "--registry", regRef, "-o", "yaml"),
			test.WithOutput(out),
			test.WithErrorOutput(logs),
		)
		r.NoError(err)

		result := out.String()
		r.Contains(result, "another-plugin")
		r.Contains(result, "Another plugin")
		r.Contains(result, "2.0.0")
	})
}
