package blob_test

import (
	"bytes"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

func TestUpdateArtifactWithInformationFromBlob_ConstructorComposition(t *testing.T) {
	for _, representation := range []string{"ordinary", "blob media type", "runtime access", "v2 access", "raw access", "unstructured access"} {
		for _, mediaType := range []string{layout.MediaTypeOCIImageLayoutTarV1, layout.MediaTypeOCIImageLayoutTarGzipV1} {
			for _, normalization := range []string{"missing", internaldigest.GenericBlobDigestV1, internaldigest.OCIArtifactDigestV1, "custom/v1"} {
				t.Run(representation+"/"+mediaType+"/"+normalization, func(t *testing.T) {
					r := require.New(t)
					reader := &countingReader{Reader: bytes.NewBufferString("archive")}
					base := inmemory.New(reader)
					resource := &descriptor.Resource{}
					switch representation {
					case "ordinary":
					case "blob media type":
						base.SetMediaType(mediaType)
					default:
						resource.Access = localBlobAccess(t, representation, mediaType)
					}
					var original *descriptor.Digest
					var snapshot descriptor.Digest
					if normalization != "missing" {
						value := digest.FromString("manifest").Encoded()
						if representation == "ordinary" && normalization == internaldigest.GenericBlobDigestV1 {
							value = digest.FromString("archive").Encoded()
						}
						original = &descriptor.Digest{
							HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
							NormalisationAlgorithm: normalization,
							Value:                  value,
						}
						snapshot = *original
						resource.Digest = original
					}
					r.NoError(ociblob.UpdateArtifactWithInformationFromBlob(resource, base))
					if representation != "ordinary" || original != nil {
						r.Zero(reader.reads)
					}
					ab, err := ociblob.NewArtifactBlob(resource, base)
					r.NoError(err)
					if representation != "ordinary" {
						r.Zero(reader.reads)
					}
					if original != nil {
						r.Same(original, resource.Digest)
						r.Equal(snapshot, *resource.Digest)
					} else if representation != "ordinary" {
						r.Nil(resource.Digest)
					} else {
						r.Equal(&descriptor.Digest{
							HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
							NormalisationAlgorithm: internaldigest.GenericBlobDigestV1,
							Value:                  digest.FromString("archive").Encoded(),
						}, resource.Digest)
					}
					dig, ok := ab.Digest()
					r.True(ok)
					r.Equal(digest.FromString("archive").String(), dig)
				})
			}
		}
	}
}

func TestUpdateArtifactWithInformationFromBlob_FrozenLayout(t *testing.T) {
	r := require.New(t)
	resource := &descriptor.Resource{
		Access: &v2.LocalBlob{MediaType: layout.MediaTypeOCIImageLayoutTarV1},
	}
	base := inmemory.New(bytes.NewBufferString("archive"))
	ab, err := ociblob.NewArtifactBlob(resource, base)
	r.NoError(err)
	resource.Access = &v2.LocalBlob{MediaType: ociImageSpecV1.MediaTypeImageManifest}
	buffered, err := ab.Buffer()
	r.NoError(err)
	r.NoError(ociblob.UpdateArtifactWithInformationFromBlob(resource, buffered))
	r.Nil(resource.Digest)
	rewrapped, err := ociblob.NewArtifactBlob(resource, buffered)
	r.NoError(err)
	r.Nil(resource.Digest)
	dig, ok := rewrapped.Digest()
	r.True(ok)
	r.Equal(digest.FromString("archive").String(), dig)
}

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
			name:         "keep existing size and update digest",
			artifact:     &descriptor.Resource{},
			blob:         inmemory.New(bytes.NewReader([]byte("test data"))),
			expectedSize: 2048,
			expectedDigest: &descriptor.Digest{
				HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
				NormalisationAlgorithm: internaldigest.GenericBlobDigestV1,
				Value:                  "916f0027a575074ce72a331777c3478d6513f786a591bd892da1a577bf2335f9",
			},
			expectError: false,
		},
		{
			name:           "source artifact (should not be updated)",
			artifact:       &descriptor.Source{},
			blob:           inmemory.New(bytes.NewReader([]byte("test data"))),
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

			if tt.expectedDigest == nil {
				assert.Nil(t, resource.Digest)
			} else {
				require.NotNil(t, resource.Digest)
				assert.Equal(t, tt.expectedDigest, resource.Digest)
			}
		})
	}
}
