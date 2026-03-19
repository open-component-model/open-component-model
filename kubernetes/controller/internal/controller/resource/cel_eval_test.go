package resource

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
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

func toJSON(t *testing.T, v any) apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return apiextensionsv1.JSON{Raw: raw}
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
			assert.Equal(t, tt.want, string(result.Raw))
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
		result, err := processAdditionalFields(ctx, env, resourceMap, map[string]apiextensionsv1.JSON{})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("flat string expressions", func(t *testing.T) {
		t.Parallel()
		result, err := processAdditionalFields(ctx, env, resourceMap, map[string]apiextensionsv1.JSON{
			"name":    toJSON(t, "resource.name"),
			"version": toJSON(t, "resource.version"),
		})
		require.NoError(t, err)
		assert.JSONEq(t, `"my-resource"`, string(result["name"].Raw))
		assert.JSONEq(t, `"3.0.0"`, string(result["version"].Raw))
	})

	t.Run("nested object", func(t *testing.T) {
		t.Parallel()
		nested, err := json.Marshal(map[string]apiextensionsv1.JSON{
			"name": toJSON(t, "resource.name"),
			"ref":  toJSON(t, "resource.access.imageReference"),
		})
		require.NoError(t, err)

		result, err := processAdditionalFields(ctx, env, resourceMap, map[string]apiextensionsv1.JSON{
			"info": {Raw: nested},
		})
		require.NoError(t, err)

		var got map[string]apiextensionsv1.JSON
		require.NoError(t, json.Unmarshal(result["info"].Raw, &got))
		assert.JSONEq(t, `"my-resource"`, string(got["name"].Raw))
		assert.JSONEq(t, `"ghcr.io/org/repo:latest"`, string(got["ref"].Raw))
	})

	t.Run("invalid value type", func(t *testing.T) {
		t.Parallel()
		_, err := processAdditionalFields(ctx, env, resourceMap, map[string]apiextensionsv1.JSON{
			"bad": {Raw: []byte(`42`)},
		})
		require.Error(t, err)
	})

	t.Run("invalid CEL expression", func(t *testing.T) {
		t.Parallel()
		_, err := processAdditionalFields(ctx, env, resourceMap, map[string]apiextensionsv1.JSON{
			"bad": toJSON(t, "resource.nonexistent.field"),
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

	nestedImages := toJSON(t, map[string]string{
		"backend":  "resource.components.backend.image",
		"frontend": "resource.components.frontend.image",
	})

	result, err := processAdditionalFields(ctx, env, resourceMap, map[string]apiextensionsv1.JSON{
		"images":  nestedImages,
		"summary": toJSON(t, `resource.components.backend.image + " " + resource.components.frontend.image`),
	})
	require.NoError(t, err)

	assert.JSONEq(t, `"ghcr.io/org/api-server:1.5.0 ghcr.io/org/web-ui:3.2.1"`, string(result["summary"].Raw))

	var images map[string]apiextensionsv1.JSON
	require.NoError(t, json.Unmarshal(result["images"].Raw, &images))
	assert.JSONEq(t, `"ghcr.io/org/api-server:1.5.0"`, string(images["backend"].Raw))
	assert.JSONEq(t, `"ghcr.io/org/web-ui:3.2.1"`, string(images["frontend"].Raw))
}
