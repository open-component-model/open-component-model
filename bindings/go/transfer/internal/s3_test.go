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
	s3accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	s3accessv2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	s3v1alpha1 "ocm.software/open-component-model/bindings/go/s3/transformation/spec/v1alpha1"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// TestProcessS3 verifies that an s3 resource into an OCI registry target is transferred by
// value through a single fused streaming node (OCIStreamLocalResource) that downloads the
// content and embeds it as a local blob in the target, and tracks that node as the resource's
// transformation. No separate download/temp-file node is emitted.
func TestProcessS3(t *testing.T) {
	s3Access := &s3accessv2.S3{
		Type:       s3accessspec.V2VersionedType,
		BucketName: "my-bucket",
		ObjectKey:  "path/to/artifact.txt",
	}
	var rawAccess runtime.Raw
	require.NoError(t, runtime.NewScheme(runtime.WithAllowUnknown()).Convert(s3Access, &rawAccess))

	resource := descriptorv2.Resource{
		ElementMeta: descriptorv2.ElementMeta{
			ObjectMeta: descriptorv2.ObjectMeta{Name: "test-s3-resource", Version: "1.0.0"},
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

	_, err := processS3(resource, "comp1", val, tgd, toSpec, resourceTransformIDs, 0)
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

// TestProcessS3_CTFTarget verifies that an s3 resource into a CTF target falls back to the
// split DownloadS3Resource -> AddLocalResource path (the blob must land on local disk
// regardless, so streaming offers no benefit) and reports the temporary file for cleanup.
func TestProcessS3_CTFTarget(t *testing.T) {
	s3Access := &s3accessv2.S3{
		Type:       s3accessspec.V2VersionedType,
		BucketName: "my-bucket",
		ObjectKey:  "path/to/artifact.txt",
	}
	var rawAccess runtime.Raw
	require.NoError(t, runtime.NewScheme(runtime.WithAllowUnknown()).Convert(s3Access, &rawAccess))

	resource := descriptorv2.Resource{
		ElementMeta: descriptorv2.ElementMeta{
			ObjectMeta: descriptorv2.ObjectMeta{Name: "test-s3-resource", Version: "1.0.0"},
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

	exprs, err := processS3(resource, "comp1", val, tgd, toSpec, resourceTransformIDs, 0)
	require.NoError(t, err)

	// The split path emits a DownloadS3Resource node and an AddLocalResource node.
	require.Len(t, tgd.Transformations, 2)
	assert.Equal(t, s3v1alpha1.DownloadS3ResourceV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.CTFAddLocalResourceV1alpha1, tgd.Transformations[1].Type)

	// The resource's tracked transformation is the Add node, which also writes the temp
	// file that must be cleaned up.
	addResourceID := tgd.Transformations[1].ID
	assert.Equal(t, addResourceID, resourceTransformIDs[0])
	require.Equal(t, []string{"${" + addResourceID + ".spec.file}"}, exprs)
}
