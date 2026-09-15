package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	pypiaccess "ocm.software/open-component-model/bindings/go/pypi/spec/access"
	pypiaccessv1alpha1 "ocm.software/open-component-model/bindings/go/pypi/spec/access/v1alpha1"
	pypiv1alpha1 "ocm.software/open-component-model/bindings/go/pypi/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

func pypiTestValue() *discoveryValue {
	return &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/test", Version: "1.0.0"},
				},
			},
		},
	}
}

// rawPyPIAccess encodes access the way a v2 descriptor carries it.
func rawPyPIAccess(t *testing.T, access *pypiaccessv1alpha1.PyPI) *runtime.Raw {
	t.Helper()
	raw := &runtime.Raw{}
	require.NoError(t, pypiaccess.Scheme.Convert(access, raw))
	return raw
}

func pypiTestAccess() *pypiaccessv1alpha1.PyPI {
	return &pypiaccessv1alpha1.PyPI{
		Type:     runtime.NewVersionedType(pypiaccessv1alpha1.Type, pypiaccessv1alpha1.Version),
		IndexURL: "https://pypi.org/simple",
		Project:  "requests",
		Version:  "2.32.3",
	}
}

func TestProcessPyPI_EmitsGetAndAdd(t *testing.T) {
	resource := descriptorv2.Resource{}
	resource.Name = "requests"
	resource.Version = "2.32.3"
	resource.Type = "pythonPackage"
	resource.Access = rawPyPIAccess(t, pypiTestAccess())

	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	toSpec := testOCIRepo("ghcr.io/target")
	ids := map[int]string{}

	err := processPyPI(resource, "root", pypiTestValue(), tgd, toSpec, ids, 0)
	require.NoError(t, err)

	require.Len(t, tgd.Transformations, 2)
	assert.Equal(t, pypiv1alpha1.GetPyPIArtifactV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, tgd.Transformations[1].Type)

	getID := tgd.Transformations[0].ID
	assert.Equal(t, "${"+getID+".output.file}", tgd.Transformations[1].Spec.Data["file"])
	assert.Equal(t, tgd.Transformations[1].ID, ids[0])

	// The Get node carries the resource itself: that is what the transformer
	// downloads, so a wrong key here would only surface at transfer time.
	getResource, ok := tgd.Transformations[0].Spec.Data["resource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "requests", getResource["name"])
	getAccess, ok := getResource["access"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "pypi/v1alpha1", getAccess["type"])
	assert.Equal(t, "2.32.3", getAccess["version"])

	// referenceName must stay an OCI repository name, so it is the resource name.
	addedResource, ok := tgd.Transformations[1].Spec.Data["resource"].(map[string]any)
	require.True(t, ok)
	addedAccess, ok := addedResource["access"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "requests", addedAccess["referenceName"])
}
