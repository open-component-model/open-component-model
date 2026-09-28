package internal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmv1alpha1 "ocm.software/open-component-model/bindings/go/helm/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	wgetv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// specImageReference returns the target image reference a TransferOCIArtifact
// (targetResource) or AddOCIArtifact (resource) transformation pushes to.
func specImageReference(t *testing.T, tr transformv1alpha1.GenericTransformation) string {
	t.Helper()
	data, err := json.Marshal(tr.Spec.Data)
	require.NoError(t, err)
	var spec struct {
		Resource struct {
			Access struct {
				ImageReference string `json:"imageReference"`
			} `json:"access"`
		} `json:"resource"`
		TargetResource struct {
			Access struct {
				ImageReference string `json:"imageReference"`
			} `json:"access"`
		} `json:"targetResource"`
	}
	require.NoError(t, json.Unmarshal(data, &spec))
	if tr.Type == ociv1alpha1.TransferOCIArtifactV1alpha1 {
		return spec.TargetResource.Access.ImageReference
	}
	return spec.Resource.Access.ImageReference
}

func transformationTypes(tgd *transformv1alpha1.TransformationGraphDefinition) []runtime.Type {
	types := make([]runtime.Type, 0, len(tgd.Transformations))
	for _, tr := range tgd.Transformations {
		types = append(types, tr.Type)
	}
	return types
}

func TestBuildGraphDefinition_OCIUploader(t *testing.T) {
	addOCIArtifact := runtime.NewVersionedType(ociv1alpha1.AddOCIArtifactType, ociv1alpha1.Version)

	manifestBlobWithoutReferenceName := dockerManifestLocalBlobResource("my-image", "1.0.0")
	manifestBlobWithoutReferenceName.Access.(*descriptorv2.LocalBlob).ReferenceName = ""

	tests := []struct {
		name      string
		target    runtime.Typed
		resource  descriptor.Resource
		copyMode  transferv1alpha1.CopyMode
		uploaders []transferv1alpha1.UploaderConfig
		wantTypes []runtime.Type
		// wantImageRef is the image reference of the node at wantImageRefAt; <resource>
		// stands for the resource's environment node path.
		wantImageRef   string
		wantImageRefAt int
		wantCleanup    int
		wantErr        string
	}{
		{
			name:           "OCI image streams to target repository regardless of copy mode",
			target:         testOCIRepo("ghcr.io/target"),
			resource:       ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
			wantImageRef:   "ghcr.io/target/org/image:v1",
			wantImageRefAt: 0,
		},
		{
			name:           "target sub path is part of the default reference",
			target:         &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io/target", SubPath: "sub"},
			resource:       ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
			wantImageRef:   "ghcr.io/target/sub/org/image:v1",
			wantImageRefAt: 0,
		},
		{
			name:           "Helm chart is converted and added as OCI artifact",
			target:         testOCIRepo("ghcr.io/target"),
			resource:       helmResource("my-chart", "1.0.0", "https://charts.example.com", "my-chart"),
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{helmv1alpha1.GetHelmChartV1alpha1, helmv1alpha1.ConvertHelmToOCIV1alpha1, addOCIArtifact, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
			wantImageRef:   "ghcr.io/target/my-chart:1.0.0",
			wantImageRefAt: 2,
			wantCleanup:    3,
		},
		{
			name:           "OCI manifest local blob keeps its reference name verbatim",
			target:         testOCIRepo("ghcr.io/target"),
			resource:       dockerManifestLocalBlobResource("my-image", "1.0.0"),
			uploaders:      ociUploaders(),
			wantTypes:      []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, addOCIArtifact, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
			wantImageRef:   "ghcr.io/target/ghcr.io/org/image:v1",
			wantImageRefAt: 1,
			wantCleanup:    1,
		},
		{
			name:      "non-manifest local blob falls through to local blob",
			target:    testOCIRepo("ghcr.io/target"),
			resource:  localBlobResource("my-resource", "1.0.0"),
			uploaders: ociUploaders(),
			wantTypes: []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
		},
		{
			name:      "manifest local blob without reference name falls through to local blob",
			target:    testOCIRepo("ghcr.io/target"),
			resource:  manifestBlobWithoutReferenceName,
			uploaders: ociUploaders(),
			wantTypes: []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType},
		},
		{
			name:      "CTF target without imageReference falls through to local blob",
			target:    testCTFRepo("/tmp/target"),
			resource:  ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			copyMode:  transferv1alpha1.CopyModeAllResources,
			uploaders: ociUploaders(),
			wantTypes: []runtime.Type{ociv1alpha1.GetOCIArtifactV1alpha1, ociv1alpha1.CTFAddLocalResourceV1alpha1, ociv1alpha1.CTFAddComponentVersionV1alpha1, FileCleanupVersionedType},
		},
		{
			name:     "CTF target with imageReference template streams to the templated reference",
			target:   testCTFRepo("/tmp/target"),
			resource: ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				ImageReference: `${"ghcr.io/mirror/" + referenceName}`,
			}},
			wantTypes:      []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.CTFAddComponentVersionV1alpha1},
			wantImageRef:   `${"ghcr.io/mirror/" + "org/image:v1"}`,
			wantImageRefAt: 0,
		},
		{
			name:     "resource and targetRepository aliases are rewritten",
			target:   testOCIRepo("ghcr.io/target"),
			resource: ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				ImageReference: `${targetRepository + "/images/" + resource.name}:${resource.version}`,
			}},
			wantTypes:      []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
			wantImageRef:   `${"ghcr.io/target" + "/images/" + <resource>.name}:${<resource>.version}`,
			wantImageRefAt: 0,
		},
		{
			name:     "targetRepository on a CTF target is rejected",
			target:   testCTFRepo("/tmp/target"),
			resource: ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				ImageReference: `${targetRepository + "/x"}`,
			}},
			wantErr: "imageReference references targetRepository, but target CTF is not an OCI registry",
		},
		{
			name:     "referenceName without a reference name is rejected",
			target:   testOCIRepo("ghcr.io/target"),
			resource: manifestBlobWithoutReferenceName,
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				ImageReference: `${"ghcr.io/mirror/" + referenceName}`,
			}},
			wantErr: "imageReference references referenceName, but resource",
		},
		{
			name:     "non-applicable OCI uploader falls through to the next uploader",
			target:   testOCIRepo("ghcr.io/target"),
			resource: wgetResource("blob", "1.0.0", "https://source.example/blob.tar"),
			uploaders: []transferv1alpha1.UploaderConfig{
				&transferv1alpha1.OCIUploaderConfig{},
				wgetUploader(t, `${"https://target.example" + url(resource.access.url).path}`),
			},
			wantTypes: []runtime.Type{wgetv1alpha1.HTTPStreamingV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
		},
		{
			name:     "OCI uploader scoped by match leaves other resources to the default handling",
			target:   testOCIRepo("ghcr.io/target"),
			resource: ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1"),
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.OCIUploaderConfig{
				MatchSpec: &transferv1alpha1.UploaderMatch{Name: "other"},
			}},
			wantTypes: []runtime.Type{ociv1alpha1.OCIAddComponentVersionV1alpha1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{tc.resource}, nil)
			resolver := testResolverFor("ocm.software/test", "1.0.0", testOCIRepo("ghcr.io/source"), desc)
			roots := testTransferRoots("ocm.software/test", "1.0.0", tc.target, resolver)
			copyMode := tc.copyMode
			if copyMode == "" {
				copyMode = transferv1alpha1.CopyModeLocalBlobResources
			}

			tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{CopyMode: copyMode}, tc.uploaders)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantTypes, transformationTypes(tgd))
			if tc.wantImageRef != "" {
				baseID := identityToTransformationID(runtime.Identity{
					descriptor.IdentityAttributeName:    "ocm.software/test",
					descriptor.IdentityAttributeVersion: "1.0.0",
				})
				want := strings.ReplaceAll(tc.wantImageRef, "<resource>", resourceNodePath(baseID, 0))
				r.Equal(want, specImageReference(t, tgd.Transformations[tc.wantImageRefAt]))
			}
			if tc.wantCleanup > 0 {
				r.Len(cleanupFileExpressions(t, findCleanupTransformation(tgd)), tc.wantCleanup)
			}
		})
	}
}
