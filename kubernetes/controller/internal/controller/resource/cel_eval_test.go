package resource

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEnv(t *testing.T) *cel.Env {
	t.Helper()
	env, err := cel.NewEnv(
		ext.Strings(),
		cel.Variable("resource", cel.DynType),
	)
	require.NoError(t, err)
	return env
}

func TestEvalCEL(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := context.Background()
	resourceMap := map[string]any{
		"name":    "my-resource",
		"version": "1.0.0",
	}

	tests := []struct {
		name    string
		expr    string
		want    string
		wantErr bool
	}{
		{name: "string field", expr: "resource.name", want: `"my-resource"`},
		{name: "concatenation", expr: `resource.name + ":" + resource.version`, want: `"my-resource:1.0.0"`},
		{name: "numeric", expr: "1 + 2", want: "3"},
		// CEL-constructed maps use map[ref.Val]ref.Val internally, which json.Marshal
		// cannot handle. This verifies that convertCELToNative correctly converts them.
		{name: "constructed map", expr: `{"name": resource.name, "version": resource.version}`, want: `{"name":"my-resource","version":"1.0.0"}`},
		{name: "constructed list", expr: `[resource.name, resource.version]`, want: `["my-resource","1.0.0"]`},
		// CEL-constructed lists of maps combine both problematic types: []ref.Val containing map[ref.Val]ref.Val.
		{name: "constructed list of maps", expr: `[{"n": resource.name}, {"v": resource.version}]`, want: `[{"n":"my-resource"},{"v":"1.0.0"}]`},
		{name: "invalid expression", expr: "invalid.!!!", wantErr: true},
		{name: "undefined variable", expr: "resource.nonexistent", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := evalCEL(ctx, env, resourceMap, tt.name, tt.expr)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			raw, err := json.Marshal(result)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(raw))
		})
	}
}

func TestProcessAdditionalFields(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := context.Background()
	resourceMap := map[string]any{
		"name":    "my-resource",
		"version": "3.0.0",
		"access": map[string]any{
			"imageReference": "ghcr.io/org/repo:latest",
		},
	}

	t.Run("empty fields", func(t *testing.T) {
		t.Parallel()
		result, err := processAdditionalFields(ctx, env, resourceMap, map[string]any{})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("flat string expressions", func(t *testing.T) {
		t.Parallel()
		result, err := processAdditionalFields(ctx, env, resourceMap, map[string]any{
			"name":    "resource.name",
			"version": "resource.version",
		})
		require.NoError(t, err)
		assert.Equal(t, "my-resource", result["name"])
		assert.Equal(t, "3.0.0", result["version"])
	})

	t.Run("nested object", func(t *testing.T) {
		t.Parallel()
		result, err := processAdditionalFields(ctx, env, resourceMap, map[string]any{
			"info": map[string]any{
				"name": "resource.name",
				"ref":  "resource.access.imageReference",
			},
		})
		require.NoError(t, err)

		info, ok := result["info"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "my-resource", info["name"])
		assert.Equal(t, "ghcr.io/org/repo:latest", info["ref"])
	})

	t.Run("invalid value type", func(t *testing.T) {
		t.Parallel()
		_, err := processAdditionalFields(ctx, env, resourceMap, map[string]any{
			"bad": 42,
		})
		require.Error(t, err)
	})

	t.Run("invalid CEL expression", func(t *testing.T) {
		t.Parallel()
		_, err := processAdditionalFields(ctx, env, resourceMap, map[string]any{
			"bad": "resource.nonexistent.field",
		})
		require.Error(t, err)
	})
}

func TestProcessAdditionalFields_MultipleResourcesNestedMap(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := context.Background()

	resourceMap := map[string]any{
		"components": map[string]any{
			"backend": map[string]any{
				"image": "ghcr.io/org/api-server:1.5.0",
			},
			"frontend": map[string]any{
				"image": "ghcr.io/org/web-ui:3.2.1",
			},
		},
	}

	result, err := processAdditionalFields(ctx, env, resourceMap, map[string]any{
		"images": map[string]any{
			"backend":  "resource.components.backend.image",
			"frontend": "resource.components.frontend.image",
		},
		"summary": `resource.components.backend.image + " " + resource.components.frontend.image`,
	})
	require.NoError(t, err)

	assert.Equal(t, "ghcr.io/org/api-server:1.5.0 ghcr.io/org/web-ui:3.2.1", result["summary"])

	images, ok := result["images"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ghcr.io/org/api-server:1.5.0", images["backend"])
	assert.Equal(t, "ghcr.io/org/web-ui:3.2.1", images["frontend"])
}
