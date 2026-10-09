package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgetv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// TestProcessWget verifies that a wget resource into an OCI registry target is transferred by
// value through a single fused streaming node (OCIStreamLocalResource) that downloads the
// content and embeds it as a local blob in the target, and tracks that node as the resource's
// transformation. No separate download/temp-file node is emitted.
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

	_, err := processWget(resource, "comp1", val, tgd, toSpec, resourceTransformIDs, 0)
	require.NoError(t, err)

	// A single fused node streams the content into the target as a local blob.
	require.Len(t, tgd.Transformations, 1)

	streamTransform := tgd.Transformations[0]
	assert.Equal(t, ociv1alpha1.OCIStreamLocalResourceV1alpha1, streamTransform.Type)
	assert.Contains(t, streamTransform.ID, "Add")
	assert.NotNil(t, streamTransform.Spec)

	// The resource's tracked transformation is the fused streaming node.
	assert.Equal(t, streamTransform.ID, resourceTransformIDs[0])
}

// TestProcessWget_CTFTarget verifies that a wget resource into a CTF target falls back to the
// split DownloadWget -> AddLocalResource path (the blob must land on local disk regardless, so
// streaming offers no benefit) and reports the temporary file for cleanup.
func TestProcessWget_CTFTarget(t *testing.T) {
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

	toSpec := &ctf.Repository{
		Type:     runtime.Type{Name: ctf.Type, Version: "v1"},
		FilePath: "/tmp/archive.ctf",
	}

	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	resourceTransformIDs := map[int]string{}

	exprs, err := processWget(resource, "comp1", val, tgd, toSpec, resourceTransformIDs, 0)
	require.NoError(t, err)

	// The split path emits a DownloadWget node and an AddLocalResource node.
	require.Len(t, tgd.Transformations, 2)
	assert.Equal(t, wgetv1alpha1.DownloadWgetResourceV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.CTFAddLocalResourceV1alpha1, tgd.Transformations[1].Type)

	// The resource's tracked transformation is the Add node, which also writes the temp
	// file that must be cleaned up.
	addResourceID := tgd.Transformations[1].ID
	assert.Equal(t, addResourceID, resourceTransformIDs[0])
	require.Equal(t, []string{"${" + addResourceID + ".spec.file}"}, exprs)
}
