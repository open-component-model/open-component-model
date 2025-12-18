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
	mockCustomSchema := &testutils.MockCustomSchema{
		Scheme: transformerScheme,
	}

	return NewBuilder(transformerScheme).
		WithTransformer(&testutils.MockGetObjectTransformer{}, mockGetObject).
		WithTransformer(&testutils.MockAddObjectTransformer{}, mockAddObject).
		WithTransformer(&testutils.MockCustomSchemaObjectTransformer{}, mockCustomSchema)
}

func TestGraphBuilder_EvaluateAndProcessGraph(t *testing.T) {
	builder := newTestBuilder(t)

	tests := []struct {
		name                 string
		transformationSpec   string
		staticAnalysisErr    require.ErrorAssertionFunc
		runtimeProcessingErr require.ErrorAssertionFunc
	}{
		{
			name: "valid graph",
			transformationSpec: `
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
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.NoError,
		},
		{
			name: "cel reference to non existing variable",
			transformationSpec: `
environment:
  name: "my-object"
  version: "1.0.0"
transformations:
- id: get1
  type: MockGetObjectTransformer/v1alpha1
  spec:
    name: "${nonExistingVariable.name}"
    version: "${environment.version}"
`,
			staticAnalysisErr: require.Error,
		},
		{
			name: "cel reference to non existing subpath of variable",
			transformationSpec: `
environment:
  name: "my-object"
  version: "1.0.0"
transformations:
- id: get1
  type: MockGetObjectTransformer/v1alpha1
  spec:
    name: "${environment.nonExistingSubpath}"
    version: "${environment.version}"
`,
			staticAnalysisErr: require.Error,
		},
		{
			name: "cel reference to variable with primitive type mismatch",
			transformationSpec: `
environment:
  number: 1
  version: "1.0.0"
transformations:
- id: get1
  type: MockGetObjectTransformer/v1alpha1
  spec:
    name: "${environment.number}"
    version: "${environment.version}"
`,
			staticAnalysisErr: require.Error,
		},
		{
			name: "cel reference to variable with structural type mismatch",
			transformationSpec: `
environment:
  object:
    key: "value"
transformations:
- id: add1
  type: MockAddObjectTransformer/v1alpha1
  spec:
    object: ${environment.object}
`,
			staticAnalysisErr: require.Error,
		},
		{
			name: "cel reference to existing variable as optional",
			transformationSpec: `
environment:
  object:
    name: "value"
transformations:
- id: add1
  type: MockAddObjectTransformer/v1alpha1
  spec:
    object: 
      name: ${environment.object.?name}
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.NoError,
		},
		{
			name: "cel reference to non-existing variable as optional with default",
			transformationSpec: `
transformations:
- id: add1
  type: MockAddObjectTransformer/v1alpha1
  spec:
    object: 
      name: "object"
- id: add2
  type: MockAddObjectTransformer/v1alpha1
  spec:
    object: 
      name: "object2"
      version: ${add1.spec.object.?version.orValue("1.0.0")}
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.NoError,
		},
		{
			name: "cel reference to non-existing variable as optional without default",
			transformationSpec: `
transformations:
- id: add1
  type: MockAddObjectTransformer/v1alpha1
  spec:
    object: 
      name: "object"
- id: add2
  type: MockAddObjectTransformer/v1alpha1
  spec:
    object: 
      name: "object2"
      version: ${add1.spec.object.?version}
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.Error,
		},
		{
			name: "cel reference to variable with partial field match",
			transformationSpec: `
environment:
  object:
    name: "object"
    version: "1.0.0"
transformations:
- id: add1
  type: MockAddObjectTransformer/v1alpha1
  spec:
    object: ${environment.object}
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.NoError,
		},
		{
			name: "cel reference to variable with partial field match and type mismatch",
			transformationSpec: `
environment:
  object:
    name: "object"
    version: 1
transformations:
- id: add1
  type: MockAddObjectTransformer/v1alpha1
  spec:
    object: ${environment.object}
`,
			staticAnalysisErr: require.Error,
		},
		{
			name: "cel reference creating cyclic dependency",
			transformationSpec: `
transformations:
- id: get1
  type: MockGetObjectTransformer/v1alpha1
  spec:
    name: "${add1.spec.object.name}"
    version: "1.0.0"
- id: add1
  type: MockAddObjectTransformer/v1alpha1
  spec:
    object:
      name: "${get1.spec.name}"
`,
			staticAnalysisErr: require.Error,
		},
		{
			name: "cel reference to self creating cyclic dependency",
			transformationSpec: `
transformations:
- id: get1
  type: MockGetObjectTransformer/v1alpha1
  spec:
    name: "object"
    version: "${get1.spec.name}"
`,
			staticAnalysisErr: require.Error,
		},
		{
			name: "field with pattern constraint valid value",
			transformationSpec: `
transformations:
- id: transform1
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.NoError,
		},
		{
			name: "field with pattern constraint valid invalue",
			transformationSpec: `
transformations:
- id: transform1
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "not-an-object"
`,
			staticAnalysisErr: require.Error,
		},
		{
			name: "field with pattern constraint valid invalue",
			transformationSpec: `
environment:
  invalidPattern: "not-an-object"
transformations:
- id: transform1
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "${environment.invalidPattern}"
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.Error,
		},
		{
			name: "field with optional value",
			transformationSpec: `
transformations:
- id: transform1
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
      oneOfStringOrNull: "a string value"
- id: transform2
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
      oneOfStringOrNull: "${transform1.spec.object.oneOfStringOrNull}"
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.NoError,
		},
		{
			name: "field with valid dyn number value",
			transformationSpec: `
transformations:
- id: transform1
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
      oneOfStringNumberOrNull: 42
- id: transform2
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
      oneOfStringNumberOrNull: "${transform1.spec.object.oneOfStringNumberOrNull}"
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.NoError,
		},
		{
			name: "field with valid dyn string value",
			transformationSpec: `
transformations:
- id: transform1
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
      oneOfStringNumberOrNull: "hello"
- id: transform2
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
      oneOfStringNumberOrNull: "${transform1.spec.object.oneOfStringNumberOrNull}"
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.NoError,
		},
		{
			name: "field with valid dyn null value",
			transformationSpec: `
transformations:
- id: transform1
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
      oneOfStringNumberOrNull: null
- id: transform2
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
      oneOfStringNumberOrNull: "${transform1.spec.object.oneOfStringNumberOrNull}"
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.NoError,
		},
		{
			name: "field with valid dyn null value from optional",
			transformationSpec: `
transformations:
- id: transform1
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
- id: transform2
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
      oneOfStringNumberOrNull: "${transform1.spec.object.?oneOfStringNumberOrNull}"
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.NoError,
		},
		{
			name: "field with invalid dyn value",
			transformationSpec: `
transformations:
- id: transform1
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
- id: transform2
  type: MockCustomSchemaObjectTransformer/v1alpha1
  spec:
    object:
      stringWithPattern: "object"
      oneOfStringNumberOrNull: "${transform1.output.object.oneOfStringNumberOrNull.nested}"
`,
			staticAnalysisErr:    require.NoError,
			runtimeProcessingErr: require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			tgd := &v1alpha1.TransformationGraphDefinition{}
			r.NoError(yaml.Unmarshal([]byte(tc.transformationSpec), tgd))
			graph, err := builder.BuildAndCheck(tgd)
			tc.staticAnalysisErr(t, err)
			if err != nil {
				r.Nil(graph)
				return
			}
			r.NotNil(graph)

			tc.runtimeProcessingErr(t, graph.Process(t.Context()))
		})
	}
}
