package builder

import (
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/graph/internal/testutils"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"sigs.k8s.io/yaml"
)

func newTestBuilder(t *testing.T) *Builder {
	t.Helper()

	transformerScheme := runtime.NewScheme()
	transformerScheme.MustRegisterScheme(testutils.Scheme)

	mockGetObject := &testutils.MockGetObject{
		Scheme: transformerScheme,
	}
	mockAddObject := &testutils.MockAddObject{
		Scheme: transformerScheme,
	}

	return NewBuilder(transformerScheme).
		WithTransformer(&testutils.MockGetObjectTransformer{}, mockGetObject).
		WithTransformer(&testutils.MockAddObjectTransformer{}, mockAddObject)
}

func TestGraphBuilder_EvaluateGraphAndAdd(t *testing.T) {
	r := require.New(t)
	builder := newTestBuilder(t)

	yamlSrc := `
environment:
  name: "my-object"
  version: "1.0.0"
transformations:
- id: get1
  type: MockGetObjectTransformer/v1alpha1
  spec:
    name: "${environment.name}"
    version: "${environment.version}"
- id: add1
  type: MockAddObjectTransformer/v1alpha1
  spec:
    object: ${get1.output.object}
`
	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(yamlSrc), tgd))
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NotNil(graph)

	r.NoError(graph.Process(t.Context()))
}
