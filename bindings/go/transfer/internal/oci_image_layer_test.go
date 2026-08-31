package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

const testLayerDigest = "sha256:1b6a62255aef35d373d870dcd1f34aeb23ffa164e20025afc56fe4695596b53d"

func layerResource(t *testing.T, accessType runtime.Type) descriptorv2.Resource {
	t.Helper()
	access := &ociaccessv1.OCIImageLayer{
		Type:      accessType,
		Reference: "ghcr.io/acme/myapp",
		MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
		Digest:    testLayerDigest,
		Size:      15040,
	}
	var rawAccess runtime.Raw
	require.NoError(t, runtime.NewScheme(runtime.WithAllowUnknown()).Convert(access, &rawAccess))

	return descriptorv2.Resource{
		ElementMeta: descriptorv2.ElementMeta{
			ObjectMeta: descriptorv2.ObjectMeta{Name: "podinfo-chart-layer", Version: "6.9.1"},
		},
		Type:     "helmChart",
		Relation: descriptorv2.ExternalRelation,
		Access:   &rawAccess,
	}
}

func layerDiscoveryValue() *discoveryValue {
	return &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/comp", Version: "1.0.0"},
				},
			},
		},
	}
}

// TestProcessOCIImageLayer verifies that a layer resource is transferred by value: it
// emits a GetOCIArtifact node followed by an AddLocalResource node, and tracks the add
// node as the resource's transformation.
func TestProcessOCIImageLayer(t *testing.T) {
	toSpec := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
	}
	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	resourceTransformIDs := map[int]string{}

	err := processOCIImageLayer(layerResource(t, runtime.NewVersionedType(ociaccessv1.OCIImageLayerType, ociaccessv1.Version)),
		"comp1", layerDiscoveryValue(), tgd, toSpec, resourceTransformIDs, 0)
	require.NoError(t, err)

	require.Len(t, tgd.Transformations, 2)

	getTransform := tgd.Transformations[0]
	assert.Equal(t, ociv1alpha1.GetOCIArtifactV1alpha1, getTransform.TransformationMeta.Type)
	assert.Contains(t, getTransform.TransformationMeta.ID, "Get")
	assert.NotNil(t, getTransform.Spec)

	addTransform := tgd.Transformations[1]
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, addTransform.TransformationMeta.Type)
	assert.Contains(t, addTransform.TransformationMeta.ID, "Add")

	assert.Equal(t, addTransform.TransformationMeta.ID, resourceTransformIDs[0])
}

// TestProcessResource_OCIImageLayer covers the routing in processResource, which skipped
// layer resources silently before they were handled.
func TestProcessResource_OCIImageLayer(t *testing.T) {
	for _, tt := range []struct {
		name       string
		accessType runtime.Type
		uploadType transferv1alpha1.UploadType
	}{
		{
			name:       "OCIImageLayer/v1",
			accessType: runtime.NewVersionedType(ociaccessv1.OCIImageLayerType, ociaccessv1.Version),
		},
		{
			name:       "legacy ociBlob/v1",
			accessType: runtime.NewVersionedType(ociaccessv1.LegacyOCIBlobAccessType, ociaccessv1.LegacyOCIBlobAccessTypeVersion),
		},
		{
			// A layer is not an artifact, so it is transferred by value even when the
			// caller asked for artifacts to be uploaded as artifacts.
			name:       "upload as OCI artifact requested",
			accessType: runtime.NewVersionedType(ociaccessv1.OCIImageLayerType, ociaccessv1.Version),
			uploadType: transferv1alpha1.UploadAsOciArtifact,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resource := layerResource(t, tt.accessType)
			access, err := scheme.NewObject(resource.Access.Type)
			require.NoError(t, err)
			require.NoError(t, scheme.Convert(resource.Access, access))
			require.IsType(t, &ociaccessv1.OCIImageLayer{}, access)

			toSpec := &oci.Repository{
				Type:    runtime.Type{Name: oci.Type, Version: "v1"},
				BaseUrl: "ghcr.io",
			}
			tgd := &transformv1alpha1.TransformationGraphDefinition{}
			resourceTransformIDs := map[int]string{}

			files, err := processResource(resource, access, "comp1", layerDiscoveryValue(), tgd, toSpec, resourceTransformIDs, 0, tt.uploadType)
			require.NoError(t, err)

			require.Len(t, tgd.Transformations, 2, "layer must not be skipped")
			assert.Equal(t, ociv1alpha1.GetOCIArtifactV1alpha1, tgd.Transformations[0].TransformationMeta.Type)
			assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, tgd.Transformations[1].TransformationMeta.Type)
			// The downloaded file is temporary and must be cleaned up afterwards.
			require.Len(t, files, 1)
			assert.Contains(t, files[0], ".spec.file")
		})
	}
}
