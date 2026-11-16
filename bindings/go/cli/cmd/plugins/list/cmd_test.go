package list_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	"ocm.software/open-component-model/bindings/go/cli/cmd/plugins/list"
	"ocm.software/open-component-model/bindings/go/dag"
)

func testVertices() []*dag.Vertex[string] {
	return []*dag.Vertex[string]{
		{
			ID: "ghcr.io/acme/second",
			Attributes: map[string]any{
				"name":        "second",
				"version":     "v0.1.0",
				"platforms":   []string{"linux/arm64"},
				"description": "",
				"registry":    "ghcr.io/acme",
			},
		},
		{
			ID: "ghcr.io/acme/first",
			Attributes: map[string]any{
				"name":        "first",
				"version":     "v1.2.3",
				"platforms":   []string{"linux/amd64", "darwin/arm64"},
				"description": "does things: with a colon",
				"registry":    "ghcr.io/acme",
			},
		},
	}
}

func TestSerializeVerticesToYAML(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, list.SerializeVerticesToYAML(&buf, testVertices()))

	var got []list.PluginInfo
	require.NoError(t, yaml.Load(buf.Bytes(), &got))
	require.Equal(t, []list.PluginInfo{
		{Name: "first", Version: "v1.2.3", Platforms: []string{"linux/amd64", "darwin/arm64"}, Description: "does things: with a colon", Registry: "ghcr.io/acme"},
		{Name: "second", Version: "v0.1.0", Platforms: []string{"linux/arm64"}, Registry: "ghcr.io/acme"},
	}, got)
}
