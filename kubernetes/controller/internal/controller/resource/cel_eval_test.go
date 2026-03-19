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
		"name":    "aggregate",
		"version": "2.0.0",
		"components": map[string]any{
			"backend": map[string]any{
				"name":    "api-server",
				"version": "1.5.0",
				"access": map[string]any{
					"type":           "ociArtifact",
					"imageReference": "ghcr.io/org/api-server:1.5.0",
				},
				"labels": map[string]any{
					"team":     "platform",
					"critical": true,
				},
			},
			"frontend": map[string]any{
				"name":    "web-ui",
				"version": "3.2.1",
				"access": map[string]any{
					"type":           "ociArtifact",
					"imageReference": "ghcr.io/org/web-ui:3.2.1",
				},
				"labels": map[string]any{
					"team":     "ui",
					"critical": false,
				},
			},
		},
	}

	// Build nested additional fields that reference both components.
	deployInfoInner, err := json.Marshal(map[string]apiextensionsv1.JSON{
		"backendImage":  toJSON(t, "resource.components.backend.access.imageReference"),
		"frontendImage": toJSON(t, "resource.components.frontend.access.imageReference"),
		"backendTeam":   toJSON(t, "resource.components.backend.labels.team"),
	})
	require.NoError(t, err)

	versionInfoInner, err := json.Marshal(map[string]apiextensionsv1.JSON{
		"aggregate": toJSON(t, "resource.version"),
		"backend":   toJSON(t, "resource.components.backend.version"),
		"frontend":  toJSON(t, "resource.components.frontend.version"),
	})
	require.NoError(t, err)

	result, err := processAdditionalFields(ctx, env, resourceMap, map[string]apiextensionsv1.JSON{
		"deploy":   {Raw: deployInfoInner},
		"versions": {Raw: versionInfoInner},
		"summary":  toJSON(t, `resource.name + " (" + resource.version + ")"`),
	})
	require.NoError(t, err)

	// Verify the flat summary field.
	assert.JSONEq(t, `"aggregate (2.0.0)"`, string(result["summary"].Raw))

	// Verify the nested deploy info.
	var deploy map[string]apiextensionsv1.JSON
	require.NoError(t, json.Unmarshal(result["deploy"].Raw, &deploy))
	assert.JSONEq(t, `"ghcr.io/org/api-server:1.5.0"`, string(deploy["backendImage"].Raw))
	assert.JSONEq(t, `"ghcr.io/org/web-ui:3.2.1"`, string(deploy["frontendImage"].Raw))
	assert.JSONEq(t, `"platform"`, string(deploy["backendTeam"].Raw))

	// Verify the nested versions info.
	var versions map[string]apiextensionsv1.JSON
	require.NoError(t, json.Unmarshal(result["versions"].Raw, &versions))
	assert.JSONEq(t, `"2.0.0"`, string(versions["aggregate"].Raw))
	assert.JSONEq(t, `"1.5.0"`, string(versions["backend"].Raw))
	assert.JSONEq(t, `"3.2.1"`, string(versions["frontend"].Raw))
}
