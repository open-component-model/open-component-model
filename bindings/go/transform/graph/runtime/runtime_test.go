package runtime

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph"
	"ocm.software/open-component-model/bindings/go/transform/graph/internal/testutils"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

type mockFailingIdentityTransformer struct {
	err error
}

func (m *mockFailingIdentityTransformer) GetCredentialConsumerIdentities(_ context.Context, _ runtime.Typed) (map[string]runtime.Identity, error) {
	return nil, m.err
}

func (m *mockFailingIdentityTransformer) Transform(_ context.Context, _ runtime.Typed, _ map[string]map[string]string) (runtime.Typed, error) {
	return nil, fmt.Errorf("should not be called")
}

type mockFailingTransformer struct{}

func (m *mockFailingTransformer) GetCredentialConsumerIdentities(_ context.Context, _ runtime.Typed) (map[string]runtime.Identity, error) {
	return nil, nil
}

func (m *mockFailingTransformer) Transform(_ context.Context, _ runtime.Typed, _ map[string]map[string]string) (runtime.Typed, error) {
	return nil, fmt.Errorf("transformer failed")
}

type mockCredentialTransformer struct {
	scheme     *runtime.Scheme
	identities map[string]runtime.Identity
	gotCreds   map[string]map[string]string
}

func (m *mockCredentialTransformer) GetCredentialConsumerIdentities(_ context.Context, _ runtime.Typed) (map[string]runtime.Identity, error) {
	return m.identities, nil
}

func (m *mockCredentialTransformer) Transform(ctx context.Context, step runtime.Typed, creds map[string]map[string]string) (runtime.Typed, error) {
	m.gotCreds = creds
	mock := &testutils.MockGetObject{Scheme: m.scheme}
	return mock.Transform(ctx, step, creds)
}

type mockCredentialResolver struct {
	creds map[string]map[string]string
	err   error
}

func (m *mockCredentialResolver) Resolve(_ context.Context, identity runtime.Identity) (map[string]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := identity[runtime.IdentityAttributeType]
	if c, ok := m.creds[key]; ok {
		return c, nil
	}
	return nil, credentials.ErrNotFound
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

func TestProcessTransformationCredentialResolution(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(testutils.Scheme)
	transformation := newTestTransformation(t)

	t.Run("single identity resolved and passed to Transform", func(t *testing.T) {
		identity := runtime.Identity{runtime.IdentityAttributeType: "test-repo"}
		mock := &mockCredentialTransformer{
			scheme:     scheme,
			identities: map[string]runtime.Identity{"repository": identity},
		}
		resolver := &mockCredentialResolver{
			creds: map[string]map[string]string{
				"test-repo": {"username": "user", "password": "pass"},
			},
		}
		rt := &Runtime{
			EvaluatedExpressionCache: map[string]any{},
			EvaluatedTransformations: map[string]any{},
			Transformers:             map[runtime.Type]Transformer{testutils.MockGetObjectV1alpha1: mock},
			CredentialProvider:       resolver,
		}
		require.NoError(t, rt.ProcessValue(t.Context(), transformation))
		require.Equal(t, map[string]map[string]string{
			"repository": {"username": "user", "password": "pass"},
		}, mock.gotCreds)
	})

	t.Run("multiple identities resolved", func(t *testing.T) {
		mock := &mockCredentialTransformer{
			scheme: scheme,
			identities: map[string]runtime.Identity{
				"source": {runtime.IdentityAttributeType: "src"},
				"target": {runtime.IdentityAttributeType: "tgt"},
			},
		}
		resolver := &mockCredentialResolver{
			creds: map[string]map[string]string{
				"src": {"username": "src-user"},
				"tgt": {"username": "tgt-user"},
			},
		}
		rt := &Runtime{
			EvaluatedExpressionCache: map[string]any{},
			EvaluatedTransformations: map[string]any{},
			Transformers:             map[runtime.Type]Transformer{testutils.MockGetObjectV1alpha1: mock},
			CredentialProvider:       resolver,
		}
		require.NoError(t, rt.ProcessValue(t.Context(), transformation))
		require.Equal(t, map[string]map[string]string{
			"source": {"username": "src-user"},
			"target": {"username": "tgt-user"},
		}, mock.gotCreds)
	})

	t.Run("nil identities passes nil credentials", func(t *testing.T) {
		mock := &mockCredentialTransformer{
			scheme:     scheme,
			identities: nil,
		}
		rt := &Runtime{
			EvaluatedExpressionCache: map[string]any{},
			EvaluatedTransformations: map[string]any{},
			Transformers:             map[runtime.Type]Transformer{testutils.MockGetObjectV1alpha1: mock},
			CredentialProvider:       &mockCredentialResolver{},
		}
		require.NoError(t, rt.ProcessValue(t.Context(), transformation))
		require.Nil(t, mock.gotCreds)
	})

	t.Run("nil credential provider passes nil credentials", func(t *testing.T) {
		identity := runtime.Identity{runtime.IdentityAttributeType: "test-repo"}
		mock := &mockCredentialTransformer{
			scheme:     scheme,
			identities: map[string]runtime.Identity{"repository": identity},
		}
		rt := &Runtime{
			EvaluatedExpressionCache: map[string]any{},
			EvaluatedTransformations: map[string]any{},
			Transformers:             map[runtime.Type]Transformer{testutils.MockGetObjectV1alpha1: mock},
			CredentialProvider:       nil,
		}
		require.NoError(t, rt.ProcessValue(t.Context(), transformation))
		require.Nil(t, mock.gotCreds)
	})

	t.Run("ErrNotFound swallowed with nil entry", func(t *testing.T) {
		identity := runtime.Identity{runtime.IdentityAttributeType: "missing"}
		mock := &mockCredentialTransformer{
			scheme:     scheme,
			identities: map[string]runtime.Identity{"repository": identity},
		}
		resolver := &mockCredentialResolver{
			creds: map[string]map[string]string{},
		}
		rt := &Runtime{
			EvaluatedExpressionCache: map[string]any{},
			EvaluatedTransformations: map[string]any{},
			Transformers:             map[runtime.Type]Transformer{testutils.MockGetObjectV1alpha1: mock},
			CredentialProvider:       resolver,
		}
		require.NoError(t, rt.ProcessValue(t.Context(), transformation))
		require.NotNil(t, mock.gotCreds)
		require.Contains(t, mock.gotCreds, "repository")
		require.Nil(t, mock.gotCreds["repository"])
	})

	t.Run("other resolve error fails transformation", func(t *testing.T) {
		identity := runtime.Identity{runtime.IdentityAttributeType: "test-repo"}
		mock := &mockCredentialTransformer{
			scheme:     scheme,
			identities: map[string]runtime.Identity{"repository": identity},
		}
		resolver := &mockCredentialResolver{
			err: fmt.Errorf("connection refused: %w", credentials.ErrUnknown),
		}
		rt := &Runtime{
			EvaluatedExpressionCache: map[string]any{},
			EvaluatedTransformations: map[string]any{},
			Transformers:             map[runtime.Type]Transformer{testutils.MockGetObjectV1alpha1: mock},
			CredentialProvider:       resolver,
		}
		err := rt.ProcessValue(t.Context(), transformation)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to resolve credentials")
	})

	t.Run("GetCredentialConsumerIdentities error fails transformation", func(t *testing.T) {
		mock := &mockFailingIdentityTransformer{
			err: fmt.Errorf("identity lookup failed"),
		}
		rt := &Runtime{
			EvaluatedExpressionCache: map[string]any{},
			EvaluatedTransformations: map[string]any{},
			Transformers:             map[runtime.Type]Transformer{testutils.MockGetObjectV1alpha1: mock},
			CredentialProvider:       &mockCredentialResolver{},
		}
		err := rt.ProcessValue(t.Context(), transformation)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get credential consumer identities")
	})
}
