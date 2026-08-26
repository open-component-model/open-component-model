package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgetv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// TestProcessWget verifies that a wget resource is transferred by value: it emits a
// DownloadWgetResource node followed by an AddLocalResource node (embedding the download as a
// local blob), and tracks the add node as the resource's transformation.
func TestProcessWget(t *testing.T) {
	wgetAccess := &wgetaccessv1.Wget{
		Type: runtime.NewVersionedType("Wget", wgetaccessv1.Version),
		URL:  "https://example.com/artifact.txt",
	}
	var rawAccess runtime.Raw
	require.NoError(t, runtime.NewScheme(runtime.WithAllowUnknown()).Convert(wgetAccess, &rawAccess))

	resource := descriptorv2.Resource{
		ElementMeta: descriptorv2.ElementMeta{
			ObjectMeta: descriptorv2.ObjectMeta{Name: "test-wget-resource", Version: "1.0.0"},
		},
		Type:     "blob",
		Relation: descriptorv2.ExternalRelation,
		Access:   &rawAccess,
	}

	val := &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/comp", Version: "1.0.0"},
				},
			},
		},
	}

	toSpec := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
	}

	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	resourceTransformIDs := map[int]string{}

	err := processWget(resource, "comp1", val, tgd, toSpec, resourceTransformIDs, 0)
	require.NoError(t, err)

	// Two nodes: download the content, then embed it as a local blob in the target.
	require.Len(t, tgd.Transformations, 2)

	getTransform := tgd.Transformations[0]
	assert.Equal(t, wgetv1alpha1.DownloadWgetResourceV1alpha1, getTransform.TransformationMeta.Type)
	assert.Contains(t, getTransform.TransformationMeta.ID, "Get")
	assert.NotNil(t, getTransform.Spec)

	addTransform := tgd.Transformations[1]
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, addTransform.TransformationMeta.Type)
	assert.Contains(t, addTransform.TransformationMeta.ID, "Add")

	// The resource's tracked transformation is the add (upload) node.
	assert.Equal(t, addTransform.TransformationMeta.ID, resourceTransformIDs[0])
}
