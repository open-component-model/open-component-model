package component_version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TODO(legacy-flags): remove together with legacy_flags.go.
func TestLegacyUploaderValues(t *testing.T) {
	for _, tc := range []struct {
		name          string
		copyResources bool
		uploadAs      string
		want          []string
	}{
		{"neither flag", false, "", nil},
		{"--upload-as localBlob is the default", false, uploadAsLocalBlob, nil},
		{"--copy-resources", true, "", []string{"localblob"}},
		{"--copy-resources --upload-as localBlob", true, uploadAsLocalBlob, []string{"localblob"}},
		{"--copy-resources --upload-as ociArtifact", true, uploadAsOCIArtifact, []string{"oci", "localblob"}},
		{"--upload-as ociArtifact only uploads OCI-manifest local blobs", false, uploadAsOCIArtifact, []string{"oci=" + legacyOCIArtifactLocalBlobMatch}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.New(t).Equal(tc.want, legacyUploaderValues(tc.copyResources, tc.uploadAs))
		})
	}
}
