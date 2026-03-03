package runtime

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph"
	"ocm.software/open-component-model/bindings/go/transform/graph/internal/testutils"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

type mockFailingTransformer struct{}

func (m *mockFailingTransformer) Transform(_ context.Context, _ runtime.Typed) (runtime.Typed, error) {
	return nil, fmt.Errorf("transformer failed")
}

func newTestRuntime(t *testing.T, transformer Transformer, events chan ProgressEvent) *Runtime {
	t.Helper()

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(testutils.Scheme)

	if transformer == nil {
		transformer = &testutils.MockGetObject{Scheme: scheme}
	}

	return &Runtime{
		EvaluatedExpressionCache: map[string]any{},
		EvaluatedTransformations: map[string]any{},
		Transformers:             map[runtime.Type]Transformer{testutils.MockGetObjectV1alpha1: transformer},
		Events:                   events,
	}
}

func newTestTransformation(t *testing.T) graph.Transformation {
	t.Helper()

	schemaJSON, err := jsonschema.UnmarshalJSON(bytes.NewReader(testutils.MockGetObjectTransformer{}.JSONSchema()))
	require.NoError(t, err)

	compiler := jsonschema.NewCompiler()
	require.NoError(t, compiler.AddResource(testutils.MockGetObjectV1alpha1.String(), schemaJSON))

	schema, err := compiler.Compile(testutils.MockGetObjectV1alpha1.String())
	require.NoError(t, err)

	return graph.Transformation{
		GenericTransformation: v1alpha1.GenericTransformation{
			TransformationMeta: meta.TransformationMeta{
				Type: testutils.MockGetObjectV1alpha1,
				ID:   "test1",
			},
			Spec: &runtime.Unstructured{Data: map[string]any{
				"name":    "test-name",
				"version": "1.0.0",
			}},
		},
		Schema: schema,
	}
}

func TestStripNullPointerValues(t *testing.T) {
	// Helper: a $ref schema (represents Go pointer-to-struct).
	refTarget := &jsonschema.Schema{}
	refSchema := &jsonschema.Schema{Ref: refTarget}

	// Helper: a string schema (represents Go value type).
	stringSchema := &jsonschema.Schema{}

	t.Run("strips nil for non-required ref property", func(t *testing.T) {
		schema := &jsonschema.Schema{
			Properties: map[string]*jsonschema.Schema{
				"provFile": refSchema,
			},
			// provFile is NOT in Required → omitempty
		}
		m := map[string]any{"provFile": nil, "other": "keep"}
		stripNullPointerValues(m, schema)

		require.NotContains(t, m, "provFile", "nil ref property not in required should be stripped")
		require.Equal(t, "keep", m["other"])
	})

	t.Run("keeps nil for required ref property", func(t *testing.T) {
		schema := &jsonschema.Schema{
			Properties: map[string]*jsonschema.Schema{
				"resource": refSchema,
			},
			Required: []string{"resource"},
		}
		m := map[string]any{"resource": nil}
		stripNullPointerValues(m, schema)

		require.Contains(t, m, "resource", "nil required ref property should be kept")
		require.Nil(t, m["resource"])
	})

	t.Run("keeps nil for non-required string property", func(t *testing.T) {
		schema := &jsonschema.Schema{
			Properties: map[string]*jsonschema.Schema{
				"version": stringSchema,
			},
		}
		m := map[string]any{"version": nil}
		stripNullPointerValues(m, schema)

		require.Contains(t, m, "version", "nil string property should be kept even if not required")
		require.Nil(t, m["version"])
	})

	t.Run("keeps nil for property not in schema", func(t *testing.T) {
		schema := &jsonschema.Schema{
			Properties: map[string]*jsonschema.Schema{},
		}
		m := map[string]any{"unknown": nil}
		stripNullPointerValues(m, schema)

		require.Contains(t, m, "unknown", "nil property not in schema should be kept")
	})

	t.Run("recurses into nested objects", func(t *testing.T) {
		innerSchema := &jsonschema.Schema{
			Properties: map[string]*jsonschema.Schema{
				"nested": refSchema,
			},
		}
		schema := &jsonschema.Schema{
			Properties: map[string]*jsonschema.Schema{
				"spec": {Ref: innerSchema},
			},
			Required: []string{"spec"},
		}
		m := map[string]any{
			"spec": map[string]any{
				"nested": nil,
				"name":   "keep",
			},
		}
		stripNullPointerValues(m, schema)

		spec := m["spec"].(map[string]any)
		require.NotContains(t, spec, "nested", "nested nil ref should be stripped")
		require.Equal(t, "keep", spec["name"])
	})

	t.Run("no-op with nil schema", func(t *testing.T) {
		m := map[string]any{"foo": nil}
		stripNullPointerValues(m, nil)

		require.Contains(t, m, "foo", "nil schema should be a no-op")
	})

	t.Run("preserves non-nil values", func(t *testing.T) {
		schema := &jsonschema.Schema{
			Properties: map[string]*jsonschema.Schema{
				"provFile": refSchema,
				"name":     stringSchema,
			},
		}
		m := map[string]any{
			"provFile": map[string]any{"uri": "/tmp/chart.tgz"},
			"name":     "test",
		}
		stripNullPointerValues(m, schema)

		require.Contains(t, m, "provFile")
		require.Contains(t, m, "name")
		require.Equal(t, "test", m["name"])
	})
}

func TestProcessValueEvents(t *testing.T) {
	transformation := newTestTransformation(t)

	t.Run("success emits Running then Completed", func(t *testing.T) {
		events := make(chan ProgressEvent, 2)
		rt := newTestRuntime(t, nil, events)

		require.NoError(t, rt.ProcessValue(t.Context(), transformation))
		require.Equal(t, Running, (<-events).State, "first event should be Running")
		require.Equal(t, Completed, (<-events).State, "second event should be Completed")
	})

	t.Run("error emits Running then Failed", func(t *testing.T) {
		events := make(chan ProgressEvent, 2)
		rt := newTestRuntime(t, &mockFailingTransformer{}, events)

		require.Error(t, rt.ProcessValue(t.Context(), transformation))
		require.Equal(t, Running, (<-events).State, "first event should be Running")
		require.Equal(t, Failed, (<-events).State, "second event should be Failed")
	})

	t.Run("nil Events channel emits nothing", func(t *testing.T) {
		rt := newTestRuntime(t, nil, nil)

		require.Nil(t, rt.Events, "Events channel should be nil")
		require.NoError(t, rt.ProcessValue(t.Context(), transformation), "ProcessValue should succeed without events")
	})
}
