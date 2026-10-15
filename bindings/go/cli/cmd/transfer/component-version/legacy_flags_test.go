package component_version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TODO(legacy-flags): remove together with legacy_flags.go.
func TestLegacyUploaderEntries(t *testing.T) {
	oci := map[string]any{"type": "oci.uploader.transfer.config.ocm.software/v1alpha1"}
	localBlob := map[string]any{"type": "localblob.uploader.transfer.config.ocm.software/v1alpha1"}
	for _, tc := range []struct {
		name          string
		copyResources bool
		uploadAs      string
		want          []map[string]any
	}{
		{"neither flag", false, "", nil},
		{"--upload-as localBlob is the default", false, uploadAsLocalBlob, nil},
		{"--copy-resources", true, "", []map[string]any{localBlob}},
		{"--copy-resources --upload-as localBlob", true, uploadAsLocalBlob, []map[string]any{localBlob}},
		{"--copy-resources --upload-as ociArtifact", true, uploadAsOCIArtifact, []map[string]any{oci, localBlob}},
		{"--upload-as ociArtifact only uploads OCI-manifest local blobs", false, uploadAsOCIArtifact, []map[string]any{
			{"type": "oci.uploader.transfer.config.ocm.software/v1alpha1", "match": legacyOCIArtifactLocalBlobMatch},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.New(t).Equal(tc.want, legacyUploaderEntries(tc.copyResources, tc.uploadAs))
		})
	}
}
