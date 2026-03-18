package resource

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
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

func TestCelValueToAny(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		val  ref.Val
		want any
	}{
		{
			name: "string",
			val:  types.String("hello"),
			want: "hello",
		},
		{
			name: "int",
			val:  types.Int(42),
			want: int64(42),
		},
		{
			name: "bool",
			val:  types.Bool(true),
			want: true,
		},
		{
			name: "list",
			val:  types.DefaultTypeAdapter.NativeToValue([]string{"a", "b"}),
			want: []any{"a", "b"},
		},
		{
			name: "map",
			val:  types.DefaultTypeAdapter.NativeToValue(map[string]string{"k": "v", "k2": "v2"}),
			want: map[string]any{"k": "v", "k2": "v2"},
		},
		{
			name: "nested map with list",
			val: types.DefaultTypeAdapter.NativeToValue(map[string]any{
				"tags": []string{"a", "b"},
				"name": "test",
			}),
			want: map[string]any{
				"tags": []any{"a", "b"},
				"name": "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := celValueToAny(tt.val)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
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
