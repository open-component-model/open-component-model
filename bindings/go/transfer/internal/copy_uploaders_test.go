package internal

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// TestBuildGraphDefinition_LocalBlobAndReferenceUploaders covers the baseline (no uploader
// selects a resource) and the local blob and reference uploaders that replace copyMode.
func TestBuildGraphDefinition_LocalBlobAndReferenceUploaders(t *testing.T) {
	localBlobNodes := []runtime.Type{ociv1alpha1.OCIGetLocalResourceV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType}
	copiedImageNodes := []runtime.Type{ociv1alpha1.GetOCIArtifactV1alpha1, ociv1alpha1.OCIAddLocalResourceV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1, FileCleanupVersionedType}
	byReference := []runtime.Type{ociv1alpha1.OCIAddComponentVersionV1alpha1}

	image := ociImageResource("my-image", "1.0.0", "oci://ghcr.io/org/image:v1")
	blob := localBlobResource("my-blob", "1.0.0")
	custom := customAccessResource("custom", "1.0.0")
	const always = "true"

	for _, tc := range []struct {
		name      string
		resource  descriptor.Resource
		uploaders []transferv1alpha1.UploaderConfig
		wantTypes []runtime.Type
		wantErr   string
	}{
		{
			name:      "baseline keeps an OCI image by reference",
			resource:  image,
			wantTypes: byReference,
		},
		{
			name:      "baseline copies a local blob",
			resource:  blob,
			wantTypes: localBlobNodes,
		},
		{
			name:      "local blob uploader copies an OCI image",
			resource:  image,
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.LocalBlobUploaderConfig{}},
			wantTypes: copiedImageNodes,
		},
		{
			name:     "reference uploader before the catch-all excludes a resource",
			resource: image,
			uploaders: []transferv1alpha1.UploaderConfig{
				&transferv1alpha1.ReferenceUploaderConfig{Match: `resource.name == "my-image"`},
				&transferv1alpha1.LocalBlobUploaderConfig{},
			},
			wantTypes: byReference,
		},
		{
			name:      "the default reference when does not select a local blob",
			resource:  blob,
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.ReferenceUploaderConfig{}},
			wantTypes: localBlobNodes,
		},
		{
			name:      "a local blob selected by a reference uploader fails the build",
			resource:  blob,
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.ReferenceUploaderConfig{Match: always}},
			wantErr:   "local blobs cannot be kept by reference",
		},
		{
			name:      "the catch-all copies a local blob the OCI uploader does not select",
			resource:  blob,
			uploaders: withLocalBlobUploader(ociUploaders()...),
			wantTypes: localBlobNodes,
		},
		{
			name:      "the OCI uploader wins over the catch-all declared after it",
			resource:  image,
			uploaders: withLocalBlobUploader(ociUploaders()...),
			wantTypes: []runtime.Type{ociv1alpha1.TransferOCIArtifactV1alpha1, ociv1alpha1.OCIAddComponentVersionV1alpha1},
		},
		{
			name:      "a local blob uploader scoped to another name leaves the resource by reference",
			resource:  image,
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.LocalBlobUploaderConfig{Match: `resource.name == "other"`}},
			wantTypes: byReference,
		},
		{
			name:      "the default local blob when does not select an unknown access type",
			resource:  custom,
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.LocalBlobUploaderConfig{}},
			wantTypes: byReference,
		},
		{
			name:      "an unknown access type selected by a local blob uploader fails the build",
			resource:  custom,
			uploaders: []transferv1alpha1.UploaderConfig{&transferv1alpha1.LocalBlobUploaderConfig{Match: always}},
			wantErr:   "local blob uploader cannot copy access type Custom/v1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			desc := testDescriptor("ocm.software/test", "1.0.0", []descriptor.Resource{tc.resource}, nil)
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
