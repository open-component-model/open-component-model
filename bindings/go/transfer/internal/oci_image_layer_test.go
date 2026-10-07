package internal

import (
	"testing"

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
	r := require.New(t)
	toSpec := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
	}
	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	resourceTransformIDs := map[int]string{}

	err := processOCIImageLayer(layerResource(t, runtime.NewVersionedType(ociaccessv1.OCIImageLayerType, ociaccessv1.Version)),
		"comp1", layerDiscoveryValue(), tgd, toSpec, resourceTransformIDs, 0)
	r.NoError(err)

	r.Len(tgd.Transformations, 2)

	getTransform := tgd.Transformations[0]
	r.Equal(ociv1alpha1.GetOCIArtifactV1alpha1, getTransform.TransformationMeta.Type)
	r.Contains(getTransform.TransformationMeta.ID, "Get")
	r.NotNil(getTransform.Spec)

	addTransform := tgd.Transformations[1]
	r.Equal(ociv1alpha1.OCIAddLocalResourceV1alpha1, addTransform.TransformationMeta.Type)
	r.Contains(addTransform.TransformationMeta.ID, "Add")

	r.Equal(addTransform.TransformationMeta.ID, resourceTransformIDs[0])
}

// TestProcessResource_OCIImageLayer covers the dispatch of the local blob uploader, which
// copies a layer under its current and its legacy type name.
func TestProcessResource_OCIImageLayer(t *testing.T) {
	for _, tt := range []struct {
		name       string
		accessType runtime.Type
	}{
		{name: "OCIImageLayer/v1", accessType: runtime.NewVersionedType(ociaccessv1.OCIImageLayerType, ociaccessv1.Version)},
		{name: "legacy ociBlob/v1", accessType: runtime.NewVersionedType(ociaccessv1.LegacyOCIBlobAccessType, ociaccessv1.LegacyOCIBlobAccessTypeVersion)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			resource := layerResource(t, tt.accessType)
			access, err := scheme.NewObject(resource.Access.Type)
			r.NoError(err)
			r.NoError(scheme.Convert(resource.Access, access))
			r.IsType(&ociaccessv1.OCIImageLayer{}, access)

			toSpec := &oci.Repository{
				Type:    runtime.Type{Name: oci.Type, Version: "v1"},
				BaseUrl: "ghcr.io",
			}
			tgd := &transformv1alpha1.TransformationGraphDefinition{}
			resourceTransformIDs := map[int]string{}

			files, err := processResource(resource, access, "comp1", layerDiscoveryValue(), tgd, toSpec, resourceTransformIDs, 0)
			r.NoError(err)

			r.Len(tgd.Transformations, 2, "layer must not be skipped")
			r.Equal(ociv1alpha1.GetOCIArtifactV1alpha1, tgd.Transformations[0].TransformationMeta.Type)
			r.Equal(ociv1alpha1.OCIAddLocalResourceV1alpha1, tgd.Transformations[1].TransformationMeta.Type)
			// The downloaded file is temporary and must be cleaned up afterwards.
			r.Len(files, 1)
			r.Contains(files[0], ".spec.file")
		})
	}
}

// TestBuildGraphDefinition_OCIImageLayerUploaders covers which uploader handles a layer. A
// layer is a blob, not an OCI artifact: the default local blob match copies it, the
// default OCI match never selects it, and an OCI uploader that is made to select it fails.
func TestBuildGraphDefinition_OCIImageLayerUploaders(t *testing.T) {
	copiedLayerNodes := []runtime.Type{ociv1alpha1.GetOCIArtifactV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType}
	byReference := []runtime.Type{ociv1alpha1.OCIAddComponentVersionV1alpha1}

	layer := descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "podinfo-chart-layer", Version: "6.9.1"}},
		Type:        "helmChart",
		Relation:    descriptor.ExternalRelation,
		Access: &ociaccessv1.OCIImageLayer{
			Type:      runtime.NewVersionedType(ociaccessv1.LegacyOCIBlobAccessType, ociaccessv1.LegacyOCIBlobAccessTypeVersion),
			Reference: "ghcr.io/acme/myapp",
			MediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
			Digest:    testLayerDigest,
			Size:      15040,
		},
	}

	for _, tc := range []struct {
		name      string
		uploaders []transferv1alpha1.UploaderConfig
		wantTypes []runtime.Type
		wantErr   string
	}{
		{
			name:      "without an uploader a layer stays by reference",
			wantTypes: byReference,
		},
		{
			name:      "the default local blob match copies a layer",
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.LocalBlobUploaderConfig{}},
			wantTypes: copiedLayerNodes,
		},
		{
			// --copy-resources --upload-as ociArtifact
			name: "the default OCI match passes a layer on to the local blob uploader",
			uploaders: []transferv1alpha1.UploaderConfig{
				&transferv1alpha1.OCIUploaderConfig{},
				&transferv1alpha1.LocalBlobUploaderConfig{},
			},
			wantTypes: copiedLayerNodes,
		},
		{
			name:      "an OCI uploader selecting a layer fails the build",
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{Match: `resource.access.isType("OCIImageLayer")`}},
			wantErr:   "oci uploader cannot upload access type ociBlob/v1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{layer}, nil)
			resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
			roots := testTransferRoots("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/target"), resolver)

			tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{}, tc.uploaders)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantTypes, transformationTypes(tgd))
		})
	}
}
