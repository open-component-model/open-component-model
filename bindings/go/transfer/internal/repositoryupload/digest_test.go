package repositoryupload

import (
	"bytes"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// A resource excluded from signing (ocm-spec: NO-DIGEST / EXCLUDE-FROM-SIGNATURE) is uploaded
// without a content check and published with its digest unchanged. Mirrors Upload's
// knownDigest -> Put -> uploadedDigest sequence.
func TestUploadDigestExcludedFromSignature(t *testing.T) {
	content := []byte("content that must not be checked")
	tests := []struct {
		name    string
		fromOCI bool
		skip    string
	}{
		{name: "plain blob source", skip: "ADR 0032: transfer adoption is outside the proof of concept"},
		{name: "OCI artifact source", fromOCI: true, skip: "ADR 0032: pending maintainer agreement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip != "" {
				t.Skip(tt.skip)
			}
			r := require.New(t)
			src := descriptor.NewExcludeFromSignatureDigest()

			expected, _, err := knownDigest(src, inmemory.New(bytes.NewReader(content)), tt.fromOCI)
			r.NoError(err)
			r.Empty(expected, "an excluded digest must not be expected of the uploaded bytes")

			r.Equal(descriptor.NewExcludeFromSignatureDigest(), uploadedDigest(src, expected, digest.FromBytes(content)))
		})
	}
}
