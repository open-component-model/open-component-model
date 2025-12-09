package builder

import (
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/transformations/oci"
	ocitransformer "ocm.software/open-component-model/bindings/go/transform/transformer/oci"
	"sigs.k8s.io/yaml"
)

func newTestBuilder(t *testing.T) *Builder {
	t.Helper()
	transformerScheme := runtime.NewScheme()
	require.NoError(t, transformerScheme.RegisterWithAlias(
		&oci.DownloadComponentTransformation{},
		oci.DownloadComponentTransformationType,
	))
	//require.NoError(t, transformerScheme.RegisterWithAlias(
	//	&transformations.UploadComponentTransformation{},
	//	runtime.NewUnversionedType(transformations.UploadComponentTransformationType),
	//))

	pm := manager.NewPluginManager(t.Context())
	repoProvider := provider.NewComponentVersionRepositoryProvider()
	repoScheme := runtime.NewScheme()
	repository.MustAddToScheme(repoScheme)
	require.NoError(t, pm.ComponentVersionRepositoryRegistry.RegisterInternalComponentVersionRepositoryPlugin(
		repoProvider,
	))

	return NewBuilder().WithTransformer(
		oci.DownloadComponentTransformationType,
		&oci.DownloadComponentTransformation{},
		&ocitransformer.DownloadComponent{
			RepoProvider: repoProvider,
		},
	)
}

// ${environment.repository.type} interpolation
func TestGraphBuilder_InterpolatesEnvironmentRepositoryType(t *testing.T) {
	r := require.New(t)
	builder := newTestBuilder(t)

	yamlSrc := `
environment:
  repository:
    type: oci
    baseUrl: "ghcr.io/test"
transformations:
- id: download
  type: ocm.software.download.component
  spec:
    repository:
      type: ${environment.repository.type}
      baseUrl: ${environment.repository.baseUrl}
    component: "A"
    version: "1.0.0"
`
	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(yamlSrc), tgd))
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NotNil(graph)
}

// ${environment.repository.type} interpolation
func TestGraphBuilder_CheckWithOptionalTypeWrapping(t *testing.T) {
	r := require.New(t)
	builder := newTestBuilder(t)

	yamlSrc := `
environment:
  repository:
    type: oci
    baseUrl: "ghcr.io/test"
transformations:
- id: download
  type: ocm.software.download.component
  spec:
    repository:
      type: ${environment.repository.type}
      baseUrl: ${environment.repository.?baseUrl}
    component: "A"
    version: "1.0.0"
`
	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(yamlSrc), tgd))
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NotNil(graph)
}

// ${environment.repository} whole object substitution
func TestGraphBuilder_SubstitutesFullEnvironmentRepository(t *testing.T) {
	r := require.New(t)
	builder := newTestBuilder(t)

	yamlSrc := `
environment:
  repository:
    type: oci
    baseUrl: "ghcr.io/fullrepo"
transformations:
- id: download
  type: ocm.software.download.component
  spec:
    repository: "${environment.repository}"
    component: "B"
    version: "2.0.0"
`

	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(yamlSrc), tgd))
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NotNil(graph)
	r.Len(graph.checked.Vertices, 1)

}

// ${environment.repository} whole object substitution
func TestGraphBuilder_StaticTypeFieldValue(t *testing.T) {
	r := require.New(t)
	builder := newTestBuilder(t)

	yamlSrc := `
environment:
  repository:
    type: oci
    baseUrl: "ghcr.io/fullrepo"
transformations:
- id: download
  type: ocm.software.download.component
  spec:
    repository:
      type: "oci"
      baseUrl: "ghcr.io/fullrepo"
    component: "B"
    version: "2.0.0"
`

	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(yamlSrc), tgd))
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NotNil(graph)
	r.Len(graph.checked.Vertices, 1)

}

// cross-node field reference ${first.spec.repository}
func TestGraphBuilder_CrossNodeFieldReference(t *testing.T) {
	r := require.New(t)
	builder := newTestBuilder(t)

	// ocm transfer cv sourcerepo//component:1.0.0 targetrepo
	// 1. first prefetch component tree
	// 2. translate component tree to transfer specification
	yamlSrc := `
environment:
  repository:
    type: oci
    baseUrl: "ghcr.io/crossnode"
transformations:
- id: first
  type: ocm.software.download.component
  spec:
    // download component version plugin
    // needs a repository spec
    // repository spec => backed by JSON Schema
    repository: "${environment.repository}"
    component: "C"
    version: "3.0.0"
- id: second
  type: ocm.software.download.component
  spec:
    repository: 
      // contains the same schema for repository as first
      type: ${first.spec.repository.type}
      baseUrl: ${first.spec.repository.baseUrl}"
    component: "${first.spec.component}"
    version: "${first.spec.version}"
`
	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(yamlSrc), tgd))
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NotNil(graph)
}

// invalid environment reference
func TestGraphBuilder_InvalidEnvironmentReferenceFails(t *testing.T) {
	r := require.New(t)
	builder := newTestBuilder(t)

	yamlSrc := `
transformations:
- id: bad
  type: ocm.software.download.component
  spec:
    repository: "${environment.missing}"
    component: "Z"
    version: "1.0.0"
`
	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(yamlSrc), tgd))
	_, err := builder.BuildAndCheck(tgd)
	r.Error(err)
}

func TestGraphBuilder_TypeChecksStaticValuesAgainstDiscoveredSchema(t *testing.T) {
	r := require.New(t)
	builder := newTestBuilder(t)

	yamlSrc := `
environment:
  repository:
    type: oci
    baseUrl: "ghcr.io/test"
transformations:
- id: download
  type: ocm.software.download.component
  spec:
    repository:
      type: ${environment.repository.type}
      baseUrl: ${environment.repository.baseUrl}
      nonExistentField: 1234
    component: "A"
    version: "1.0.0"
`
	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(yamlSrc), tgd))
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NotNil(graph)
}

func TestGraphBuilder_EvaluateGraph(t *testing.T) {
	r := require.New(t)
	builder := newTestBuilder(t)

	yamlSrc := `
environment:
  repository:
    type: ctf
    filePath: "/home/jakob/Projects/worktrees/open-component-model/transformadness/bindings/go/transform/graph/test/transport-archive"
    accessMode: "readwrite"
transformations:
- id: download1
  type: ocm.software.download.component.oci
  spec:
    repository:
      type: ${environment.repository.type}
      filePath: ${environment.repository.filePath}
      accessMode: ${environment.repository.accessMode}
    component: "github.com/acme.org/helloworld"
    version: "1.0.0"
- id: download2
  type: ocm.software.download.component.oci
  spec:
    repository: ${download1.spec.repository}
    component: ${download1.spec.component}
    version: ${download1.spec.version}
`
	tgd := &v1alpha1.TransformationGraphDefinition{}
	r.NoError(yaml.Unmarshal([]byte(yamlSrc), tgd))
	graph, err := builder.BuildAndCheck(tgd)
	r.NoError(err)
	r.NotNil(graph)

	r.NoError(graph.Process(t.Context()))
}
