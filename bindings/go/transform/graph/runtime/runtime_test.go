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
