package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ctfrepo "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	s3accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	s3accessv2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	wgetv1alpha1 "ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// TestProcessResource_PlainBlobSources verifies how plain blob sources are copied as local
// blobs: a wget GET and an S3 object are streamed by one fused node that leaves no file to
// clean up, while a wget request with side effects is downloaded to a file once.
func TestProcessResource_PlainBlobSources(t *testing.T) {
	wget := func(verb string) runtime.Typed {
		return &wgetaccessv1.Wget{
			Type: runtime.NewVersionedType("Wget", wgetaccessv1.Version),
			URL:  "https://example.com/artifact.txt",
			Verb: verb,
		}
	}
	s3 := &s3accessv2.S3{Type: s3accessspec.V2VersionedType, BucketName: "my-bucket", ObjectKey: "path/to/artifact.txt"}
	ociTarget := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io"}
	ctfTarget := &ctfrepo.Repository{Type: runtime.Type{Name: ctfrepo.Type, Version: "v1"}, FilePath: "./archive"}

	tests := []struct {
		name      string
		access    runtime.Typed
		target    runtime.Typed
		wantTypes []runtime.Type
		buffered  bool
	}{
		{"wget GET to OCI", wget(""), ociTarget, []runtime.Type{uploadv1alpha1.TransferWgetToOCILocalBlobV1alpha1}, false},
		{"wget lower-case get to CTF", wget("get"), ctfTarget, []runtime.Type{uploadv1alpha1.TransferWgetToCTFLocalBlobV1alpha1}, false},
		{"wget POST to OCI", wget("POST"), ociTarget, []runtime.Type{wgetv1alpha1.DownloadWgetResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1}, true},
		{"s3 to OCI", s3, ociTarget, []runtime.Type{uploadv1alpha1.TransferS3ToOCILocalBlobV1alpha1}, false},
		{"s3 to CTF", s3, ctfTarget, []runtime.Type{uploadv1alpha1.TransferS3ToCTFLocalBlobV1alpha1}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rawAccess runtime.Raw
			require.NoError(t, runtime.NewScheme(runtime.WithAllowUnknown()).Convert(tc.access, &rawAccess))
			resource := descriptorv2.Resource{
				ElementMeta: descriptorv2.ElementMeta{
					ObjectMeta: descriptorv2.ObjectMeta{Name: "artifact", Version: "1.0.0"},
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
			tgd := &transformv1alpha1.TransformationGraphDefinition{}
			resourceTransformIDs := map[int]string{}

			fileExpressions, err := processResource(t.Context(), resource, tc.access, "comp1", val, tgd, tc.target, resourceTransformIDs, 0)
			require.NoError(t, err)

			var types []runtime.Type
			for _, transformation := range tgd.Transformations {
				types = append(types, transformation.Type)
			}
			assert.Equal(t, tc.wantTypes, types)
			assert.Equal(t, tc.buffered, len(fileExpressions) > 0, "file buffers to clean up")

			last := tgd.Transformations[len(tgd.Transformations)-1]
			assert.Equal(t, last.ID, resourceTransformIDs[0])
			if !tc.buffered {
				// referenceName is an OCI-only legacy field; a plain blob must not carry it.
				access := last.Spec.Data["targetResource"].(map[string]any)["access"].(map[string]any)
				assert.NotContains(t, access, "referenceName")
				assert.Equal(t, "ocm.software/comp", last.Spec.Data["component"])
			}
		})
	}
}
