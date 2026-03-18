package resource

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// newTestEnv creates a minimal CEL environment with a "resource" variable for testing.
func newTestEnv(t *testing.T) *cel.Env {
	t.Helper()
	env, err := cel.NewEnv(
		ext.Strings(),
		cel.Variable("resource", cel.DynType),
	)
	if err != nil {
		t.Fatalf("failed to create CEL env: %v", err)
	}
	return env
}

func toJSON(t *testing.T, v any) apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal JSON: %v", err)
	}
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
		{
			name: "simple string field",
			expr: "resource.name",
			want: `"my-resource"`,
		},
		{
			name: "string concatenation",
			expr: `resource.name + ":" + resource.version`,
			want: `"my-resource:1.0.0"`,
		},
		{
			name: "numeric expression",
			expr: "1 + 2",
			want: "3",
		},
		{
			name:    "invalid expression",
			expr:    "invalid.!!!",
			wantErr: true,
		},
		{
			name:    "undefined variable",
			expr:    "resource.nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := evalCEL(ctx, env, resourceMap, tt.name, tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(result.Raw) != tt.want {
				t.Errorf("got %s, want %s", string(result.Raw), tt.want)
			}
		})
	}
}

func TestProcessField_StringExpression(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := context.Background()
	resourceMap := map[string]any{
		"name": "test-resource",
	}

	val := toJSON(t, "resource.name")
	result, err := processField(ctx, env, resourceMap, "myField", val)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Raw) != `"test-resource"` {
		t.Errorf("got %s, want %q", string(result.Raw), `"test-resource"`)
	}
}

func TestProcessField_NestedObject(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := context.Background()
	resourceMap := map[string]any{
		"name":    "test-resource",
		"version": "2.0.0",
	}

	nested := map[string]apiextensionsv1.JSON{
		"name":    toJSON(t, "resource.name"),
		"version": toJSON(t, "resource.version"),
	}
	raw, err := json.Marshal(nested)
	if err != nil {
		t.Fatalf("failed to marshal nested: %v", err)
	}
	val := apiextensionsv1.JSON{Raw: raw}

	result, err := processField(ctx, env, resourceMap, "info", val)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]apiextensionsv1.JSON
	if err := json.Unmarshal(result.Raw, &got); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if string(got["name"].Raw) != `"test-resource"` {
		t.Errorf("name: got %s, want %q", string(got["name"].Raw), `"test-resource"`)
	}
	if string(got["version"].Raw) != `"2.0.0"` {
		t.Errorf("version: got %s, want %q", string(got["version"].Raw), `"2.0.0"`)
	}
}

func TestProcessField_InvalidValue(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := context.Background()
	resourceMap := map[string]any{}

	// A JSON number is neither a string nor an object.
	val := apiextensionsv1.JSON{Raw: []byte(`42`)}
	_, err := processField(ctx, env, resourceMap, "bad", val)
	if err == nil {
		t.Fatal("expected error for non-string, non-object value")
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

	tests := []struct {
		name   string
		fields map[string]apiextensionsv1.JSON
		verify func(t *testing.T, result map[string]apiextensionsv1.JSON)
	}{
		{
			name:   "empty fields",
			fields: map[string]apiextensionsv1.JSON{},
			verify: func(t *testing.T, result map[string]apiextensionsv1.JSON) {
				if len(result) != 0 {
					t.Errorf("expected empty result, got %d entries", len(result))
				}
			},
		},
		{
			name: "flat string expressions",
			fields: map[string]apiextensionsv1.JSON{
				"name":    toJSON(t, "resource.name"),
				"version": toJSON(t, "resource.version"),
			},
			verify: func(t *testing.T, result map[string]apiextensionsv1.JSON) {
				if string(result["name"].Raw) != `"my-resource"` {
					t.Errorf("name: got %s, want %q", string(result["name"].Raw), `"my-resource"`)
				}
				if string(result["version"].Raw) != `"3.0.0"` {
					t.Errorf("version: got %s, want %q", string(result["version"].Raw), `"3.0.0"`)
				}
			},
		},
		{
			name: "nested object with string expressions",
			fields: map[string]apiextensionsv1.JSON{
				"info": {Raw: mustMarshalForTest(t, map[string]apiextensionsv1.JSON{
					"name":    toJSON(t, "resource.name"),
					"version": toJSON(t, "resource.version"),
				})},
			},
			verify: func(t *testing.T, result map[string]apiextensionsv1.JSON) {
				var nested map[string]apiextensionsv1.JSON
				if err := json.Unmarshal(result["info"].Raw, &nested); err != nil {
					t.Fatalf("failed to unmarshal nested: %v", err)
				}
				if string(nested["name"].Raw) != `"my-resource"` {
					t.Errorf("info.name: got %s, want %q", string(nested["name"].Raw), `"my-resource"`)
				}
				if string(nested["version"].Raw) != `"3.0.0"` {
					t.Errorf("info.version: got %s, want %q", string(nested["version"].Raw), `"3.0.0"`)
				}
			},
		},
		{
			name: "mixed flat and nested",
			fields: map[string]apiextensionsv1.JSON{
				"name": toJSON(t, "resource.name"),
				"access": {Raw: mustMarshalForTest(t, map[string]apiextensionsv1.JSON{
					"ref": toJSON(t, "resource.access.imageReference"),
				})},
			},
			verify: func(t *testing.T, result map[string]apiextensionsv1.JSON) {
				if string(result["name"].Raw) != `"my-resource"` {
					t.Errorf("name: got %s, want %q", string(result["name"].Raw), `"my-resource"`)
				}
				var nested map[string]apiextensionsv1.JSON
				if err := json.Unmarshal(result["access"].Raw, &nested); err != nil {
					t.Fatalf("failed to unmarshal nested access: %v", err)
				}
				if string(nested["ref"].Raw) != `"ghcr.io/org/repo:latest"` {
					t.Errorf("access.ref: got %s, want %q", string(nested["ref"].Raw), `"ghcr.io/org/repo:latest"`)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := processAdditionalFields(ctx, env, resourceMap, tt.fields)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.verify(t, result)
		})
	}
}

func TestProcessAdditionalFields_Error(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	ctx := context.Background()
	resourceMap := map[string]any{}

	fields := map[string]apiextensionsv1.JSON{
		"good": toJSON(t, `"literal string"`),
		"bad":  toJSON(t, "resource.nonexistent.field"),
	}
	_, err := processAdditionalFields(ctx, env, resourceMap, fields)
	if err == nil {
		t.Fatal("expected error for invalid CEL expression")
	}
}

func mustMarshalForTest(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return raw
}
