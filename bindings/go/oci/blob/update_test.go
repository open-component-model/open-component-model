package blob_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
)

func TestUpdateArtifactWithInformationFromBlob(t *testing.T) {
	tests := []struct {
		name           string
		artifact       descriptor.Artifact
		blob           blob.ReadOnlyBlob
		expectedSize   int64
		expectedDigest *descriptor.Digest
		expectError    bool
	}{
		{
			name: "keep existing size and update digest",
			artifact: &descriptor.Resource{
				Size: 2048,
			},
			blob:         blob.NewDirectReadOnlyBlob(bytes.NewReader([]byte("test data"))),
			expectedSize: 2048,
			expectedDigest: &descriptor.Digest{
				HashAlgorithm: internaldigest.HashAlgorithmSHA256,
				Value:         "916f0027a575074ce72a331777c3478d6513f786a591bd892da1a577bf2335f9",
			},
			expectError: false,
		},
		{
			name:           "source artifact (should not be updated)",
			artifact:       &descriptor.Source{},
			blob:           blob.NewDirectReadOnlyBlob(bytes.NewReader([]byte("test data"))),
			expectedSize:   0,
			expectedDigest: nil,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ociblob.UpdateArtifactWithInformationFromBlob(tt.artifact, tt.blob)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			resource, ok := tt.artifact.(*descriptor.Resource)
			if !ok {
				// For source artifacts, we expect no changes
				_, ok := tt.artifact.(*descriptor.Source)
				require.True(t, ok)
				return
			}

			assert.Equal(t, tt.expectedSize, resource.Size)
			if tt.expectedDigest == nil {
				assert.Nil(t, resource.Digest)
			} else {
				require.NotNil(t, resource.Digest)
				assert.Equal(t, tt.expectedDigest.HashAlgorithm, resource.Digest.HashAlgorithm)
				assert.Equal(t, tt.expectedDigest.Value, resource.Digest.Value)
			}
		})
	}
}
