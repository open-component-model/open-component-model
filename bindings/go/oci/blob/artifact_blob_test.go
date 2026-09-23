package blob_test

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	internaldigest "ocm.software/open-component-model/bindings/go/oci/internal/digest"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockBlob implements blob.ReadOnlyBlob for testing purposes
type mockBlob struct {
	blob.ReadOnlyBlob
}

func TestNewResourceBlob(t *testing.T) {
	resource := &descriptor.Resource{
		Digest: &descriptor.Digest{
			HashAlgorithm: internaldigest.HashAlgorithmSHA256,
			Value:         "1234567890abcdef",
		},
	}
	mock := &mockBlob{}
	mediaType := "application/octet-stream"

	rb, err := ociblob.NewArtifactBlobWithMediaType(resource, mock, mediaType)
	require.NoError(t, err)
	assert.NotNil(t, rb)
	assert.Equal(t, resource, rb.Artifact)
	got, ok := rb.MediaType()
	assert.True(t, ok)
	assert.Equal(t, mediaType, got)
}

func TestResourceBlob_MediaType(t *testing.T) {
	resource := &descriptor.Resource{}
	mock := &mockBlob{}
	mediaType := "application/octet-stream"

	rb, err := ociblob.NewArtifactBlobWithMediaType(resource, mock, mediaType)
	require.NoError(t, err)
	mt, ok := rb.MediaType()
	assert.True(t, ok)
	assert.Equal(t, mediaType, mt)
}

func TestResourceBlob_Digest(t *testing.T) {
	tests := []struct {
		name           string
		resource       *descriptor.Resource
		expectedDigest string
		expectedOK     bool
	}{
		{
			name: "valid sha256 digest",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: internaldigest.HashAlgorithmSHA256,
					Value:         "1234567890abcdef",
				},
			},
			expectedDigest: "sha256:1234567890abcdef",
			expectedOK:     true,
		},
		{
			name: "empty hash algorithm defaults to canonical",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: internaldigest.HashAlgorithmSHA256,
					Value:         "1234567890abcdef",
				},
			},
			expectedDigest: "sha256:1234567890abcdef",
			expectedOK:     true,
		},
		{
			name:           "nil digest",
			resource:       &descriptor.Resource{},
			expectedDigest: "",
			expectedOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{}
			rb, err := ociblob.NewArtifactBlobWithMediaType(tt.resource, mock, "application/octet-stream")
			assert.NoError(t, err)
			dig, ok := rb.Digest()
			assert.Equal(t, tt.expectedOK, ok)
			if tt.expectedOK {
				assert.Equal(t, tt.expectedDigest, dig)
			}
		})
	}
}

func TestResourceBlob_HasPrecalculatedDigest(t *testing.T) {
	tests := []struct {
		name     string
		resource *descriptor.Resource
		expected bool
	}{
		{
			name:     "nil digest",
			resource: &descriptor.Resource{},
			expected: false,
		},
		{
			name: "empty digest value",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: internaldigest.HashAlgorithmSHA256,
					Value:         "",
				},
			},
			expected: false,
		},
		{
			name: "valid digest",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: internaldigest.HashAlgorithmSHA256,
					Value:         "1234567890abcdef",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{}
			rb, err := ociblob.NewArtifactBlobWithMediaType(tt.resource, mock, "application/octet-stream")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, rb.HasPrecalculatedDigest())
		})
	}
}

func TestResourceBlob_SetPrecalculatedDigest(t *testing.T) {
	tests := []struct {
		name      string
		resource  *descriptor.Resource
		newDigest string

		expectPanic bool
	}{
		{
			name: "existing digest in resource",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
					NormalisationAlgorithm: internaldigest.OCIArtifactDigestV1,
					Value:                  digest.FromString("manifest").Encoded(),
				},
			},
			newDigest: digest.FromString("test").String(),

			expectPanic: false,
		},
		{
			name:      "no resource digest",
			resource:  &descriptor.Resource{},
			newDigest: digest.FromString("test").String(),
		},
		{
			name:        "invalid digest format",
			resource:    &descriptor.Resource{},
			newDigest:   "invalid-digest",
			expectPanic: true,
		},
		{
			name:        "nil digest in resource",
			resource:    &descriptor.Resource{},
			newDigest:   "sha256:1234567890abcdef",
			expectPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{}
			rb, err := ociblob.NewArtifactBlobWithMediaType(tt.resource, mock, "application/octet-stream")
			require.NoError(t, err)

			original := tt.resource.Digest
			var snapshot descriptor.Digest
			if original != nil {
				snapshot = *original
			}
			if tt.expectPanic {
				assert.Panics(t, func() {
					rb.SetPrecalculatedDigest(tt.newDigest)
				})
			} else {
				rb.SetPrecalculatedDigest(tt.newDigest)
				dig, ok := rb.Digest()
				require.True(t, ok)
				require.Equal(t, tt.newDigest, dig)
				require.True(t, rb.HasPrecalculatedDigest())
			}
			if original != nil {
				require.Same(t, original, tt.resource.Digest)
				require.Equal(t, snapshot, *tt.resource.Digest)
			} else {
				require.Nil(t, tt.resource.Digest)
			}
		})
	}
}

func TestResourceBlob_OCIDescriptor(t *testing.T) {
	tests := []struct {
		name           string
		resource       *descriptor.Resource
		mediaType      string
		expectedDigest string
		expectedSize   int64
	}{
		{
			name: "valid descriptor",
			resource: &descriptor.Resource{
				Digest: &descriptor.Digest{
					HashAlgorithm: internaldigest.HashAlgorithmSHA256,
					Value:         "1234567890abcdef",
				},
			},
			mediaType:      "application/octet-stream",
			expectedDigest: "sha256:1234567890abcdef",
			expectedSize:   blob.SizeUnknown,
		},
		{
			name:           "nil digest",
			resource:       &descriptor.Resource{},
			mediaType:      "application/octet-stream",
			expectedDigest: "",
			expectedSize:   blob.SizeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{}
			rb, err := ociblob.NewArtifactBlobWithMediaType(tt.resource, mock, tt.mediaType)
			require.NoError(t, err)
			desc := rb.OCIDescriptor()

			assert.Equal(t, tt.mediaType, desc.MediaType)
			assert.Equal(t, tt.expectedSize, desc.Size)
			if tt.expectedDigest != "" {
				assert.Equal(t, digest.Digest(tt.expectedDigest), desc.Digest)
			}
		})
	}
}

func TestResourceBlob_CompleteWorkflow(t *testing.T) {
	// Test a complete workflow using ArtifactBlob
	resource := &descriptor.Resource{
		Digest: &descriptor.Digest{
			HashAlgorithm: internaldigest.HashAlgorithmSHA256,
			Value:         "1234567890abcdef",
		},
	}
	mock := &mockBlob{}
	mediaType := "application/octet-stream"

	rb, err := ociblob.NewArtifactBlobWithMediaType(resource, mock, mediaType)
	require.NoError(t, err)

	// Test all methods in sequence
	mt, ok := rb.MediaType()
	require.True(t, ok)
	assert.Equal(t, mediaType, mt)

	dig, ok := rb.Digest()
	require.True(t, ok)
	assert.Equal(t, "sha256:1234567890abcdef", dig)

	assert.True(t, rb.HasPrecalculatedDigest())

	// Update values
	newDigest := digest.FromString("test")
	rb.SetPrecalculatedDigest(newDigest.String())

	// Verify updates
	dig, ok = rb.Digest()
	require.True(t, ok)
	assert.Equal(t, newDigest.String(), dig)
	assert.Equal(t, blob.SizeUnknown, rb.Size())

	// Test OCI descriptor
	desc := rb.OCIDescriptor()
	assert.Equal(t, mediaType, desc.MediaType)
	assert.Equal(t, digest.Digest(newDigest), desc.Digest)
	assert.Equal(t, blob.SizeUnknown, desc.Size)
}

func TestNewResourceBlobWithMediaType_SizeValidation(t *testing.T) {
	tests := []struct {
		name          string
		blobSize      int64
		expectedError bool
	}{
		{
			name:          "valid blob size",
			blobSize:      100,
			expectedError: false,
		},
		{
			name:          "unknown blob size",
			blobSize:      blob.SizeUnknown,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := &descriptor.Resource{}

			base := &mockSizeAwareBlob{size: tt.blobSize}
			require.NoError(t, ociblob.UpdateArtifactWithInformationFromBlob(resource, base))
			_, err := ociblob.NewArtifactBlobWithMediaType(resource, base, "application/octet-stream")
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewResourceBlobWithMediaType_DigestValidation(t *testing.T) {
	tests := []struct {
		name           string
		resourceDigest *descriptor.Digest
		blobDigest     string
		expectedError  bool
	}{
		{
			name: "matching digests",
			resourceDigest: &descriptor.Digest{
				HashAlgorithm: internaldigest.HashAlgorithmSHA256,
				Value:         "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
			blobDigest:    "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			expectedError: false,
		},
		{
			name: "mismatched digests",
			resourceDigest: &descriptor.Digest{
				HashAlgorithm: internaldigest.HashAlgorithmSHA256,
				Value:         "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
			blobDigest:    "sha256:differentdigest",
			expectedError: true,
		},
		{
			name:           "nil resource digest with valid blob digest",
			resourceDigest: nil,
			blobDigest:     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			expectedError:  false,
		},
		{
			name: "valid resource digest with empty blob digest",
			resourceDigest: &descriptor.Digest{
				HashAlgorithm: internaldigest.HashAlgorithmSHA256,
				Value:         "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
			blobDigest:    "",
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := &descriptor.Resource{
				Digest: tt.resourceDigest,
			}

			base := &mockDigestAwareBlob{digest: tt.blobDigest}
			require.NoError(t, ociblob.UpdateArtifactWithInformationFromBlob(resource, base))
			_, err := ociblob.NewArtifactBlobWithMediaType(resource, base, "application/octet-stream")
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewResourceBlobWithMediaType_MediaTypeHandling(t *testing.T) {
	tests := []struct {
		name         string
		providedType string
		blobType     string
		expectedType string
	}{
		{
			name:         "provided media type takes precedence",
			providedType: "application/custom",
			blobType:     "application/octet-stream",
			expectedType: "application/custom",
		},
		{
			name:         "use blob media type when none provided",
			providedType: "",
			blobType:     "application/octet-stream",
			expectedType: "application/octet-stream",
		},
		{
			name:         "empty media type when neither provided",
			providedType: "",
			blobType:     "",
			expectedType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := &descriptor.Resource{}

			rb, err := ociblob.NewArtifactBlobWithMediaType(resource, &mockMediaTypeAwareBlob{mediaType: tt.blobType}, tt.providedType)
			require.NoError(t, err)
			mt, ok := rb.MediaType()
			if tt.expectedType == "" {
				assert.False(t, ok)
			} else {
				assert.True(t, ok)
				assert.Equal(t, tt.expectedType, mt)
			}
		})
	}
}

func TestArtifactBlob_LayoutDigestSeparation(t *testing.T) {
	for _, normalization := range []string{internaldigest.OCIArtifactDigestV1, internaldigest.GenericBlobDigestV1, ""} {
		for _, mediaType := range []string{layout.MediaTypeOCIImageLayoutTarV1, layout.MediaTypeOCIImageLayoutTarGzipV1} {
			for _, location := range []string{"explicit", "underlying", "runtime access", "v2 access", "raw access", "unstructured access"} {
				for _, known := range []bool{false, true} {
					name := normalization + "/" + mediaType + "/" + location
					if known {
						name += "/known checksum"
					}
					t.Run(name, func(t *testing.T) {
						r := require.New(t)
						manifest := &descriptor.Digest{
							HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
							NormalisationAlgorithm: normalization,
							Value:                  digest.FromString("manifest").Encoded(),
						}
						original := *manifest
						resource := &descriptor.Resource{Digest: manifest}
						explicitType, underlyingType := "application/octet-stream", "application/octet-stream"
						switch location {
						case "explicit":
							explicitType = mediaType
						case "underlying":
							underlyingType = mediaType
						default:
							resource.Access = localBlobAccess(t, location, mediaType)
						}
						base := direct.NewFromBytes([]byte("archive"), direct.WithMediaType(underlyingType))
						var b blob.ReadOnlyBlob = base
						checksum := digest.FromString("archive").String()
						if known {
							b = &mockDigestAwareMediaBlob{Blob: base, checksum: checksum}
						}
						ab, err := ociblob.NewArtifactBlobWithMediaType(resource, b, explicitType)
						r.NoError(err)
						dig, ok := ab.Digest()
						r.Equal(known, ok)
						r.Equal(known, ab.HasPrecalculatedDigest())
						if known {
							r.Equal(checksum, dig)
						} else {
							r.Empty(dig)
						}
						// Packing replaces the archive access with the root manifest access.
						resource.Access = &v2.LocalBlob{MediaType: ociImageSpecV1.MediaTypeImageManifest}
						dig, ok = ab.Digest()
						r.Equal(known, ok)
						r.Equal(known, ab.HasPrecalculatedDigest())
						if known {
							r.Equal(checksum, dig)
						} else {
							r.Empty(dig)
						}
						buffered, err := ab.Buffer()
						r.NoError(err)
						dig, ok = buffered.Digest()
						r.True(ok)
						r.Equal(checksum, dig)
						mt, ok := buffered.MediaType()
						r.True(ok)
						r.Equal(explicitType, mt)
						// Keep the same bytes but remove the cached checksum to exercise
						// the buffered wrapper's frozen classification as well.
						buffered.ReadOnlyBlob = base
						dig, ok = buffered.Digest()
						r.False(ok)
						r.Empty(dig)
						r.False(buffered.HasPrecalculatedDigest())
						r.Same(manifest, resource.Digest)
						r.Equal(original, *resource.Digest)
					})
				}
			}
		}
	}
}

func TestArtifactBlob_DigestDefaulting(t *testing.T) {
	for _, mediaType := range []string{"application/octet-stream", layout.MediaTypeOCIImageLayoutTarV1, layout.MediaTypeOCIImageLayoutTarGzipV1} {
		for _, fromAccess := range []bool{false, true} {
			t.Run(mediaType+"/"+map[bool]string{false: "explicit", true: "access"}[fromAccess], func(t *testing.T) {
				r := require.New(t)
				resource := &descriptor.Resource{}
				explicitType := mediaType
				if fromAccess {
					explicitType = "application/octet-stream"
					resource.Access = &v2.LocalBlob{MediaType: mediaType}
				}
				checksum := digest.FromString("archive")
				ab, err := ociblob.NewArtifactBlobWithMediaType(resource, &mockDigestAwareBlob{digest: checksum.String()}, explicitType)
				r.NoError(err)
				dig, ok := ab.Digest()
				r.True(ok)
				r.Equal(checksum.String(), dig)
				if mediaType == "application/octet-stream" {
					r.Equal(&descriptor.Digest{
						HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
						NormalisationAlgorithm: internaldigest.GenericBlobDigestV1,
						Value:                  checksum.Encoded(),
					}, resource.Digest)
				} else {
					r.Nil(resource.Digest)
				}
			})
		}
	}
}

func TestArtifactBlob_OrdinaryDigestNormalization(t *testing.T) {
	for _, normalization := range []string{"", internaldigest.GenericBlobDigestV1, internaldigest.OCIArtifactDigestV1, "custom/v1"} {
		for _, known := range []bool{false, true} {
			t.Run(normalization+"/"+map[bool]string{false: "unknown", true: "known"}[known], func(t *testing.T) {
				r := require.New(t)
				resource := &descriptor.Resource{Digest: &descriptor.Digest{
					HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
					NormalisationAlgorithm: normalization,
					Value:                  digest.FromString("normalized").Encoded(),
				}}
				base := &mockDigestAwareBlob{}
				if known {
					base.digest = digest.FromString("bytes").String()
				}
				ab, err := ociblob.NewArtifactBlob(resource, base)
				generic := normalization == "" || normalization == internaldigest.GenericBlobDigestV1
				if generic && known {
					r.ErrorContains(err, "resource blob digest mismatch")
					return
				}
				r.NoError(err)
				dig, ok := ab.Digest()
				r.Equal(known || generic, ok)
				r.Equal(ok, ab.HasPrecalculatedDigest())
				switch {
				case known:
					r.Equal(base.digest, dig)
				case generic:
					r.Equal(digest.FromString("normalized").String(), dig)
				default:
					r.Empty(dig)
				}
			})
		}
	}
}

func TestArtifactBlob_BufferDigestSeparation(t *testing.T) {
	for _, mediaType := range []string{"application/octet-stream", layout.MediaTypeOCIImageLayoutTarV1, layout.MediaTypeOCIImageLayoutTarGzipV1} {
		for _, normalization := range []string{"", internaldigest.GenericBlobDigestV1, internaldigest.OCIArtifactDigestV1} {
			t.Run(mediaType+"/"+normalization, func(t *testing.T) {
				r := require.New(t)
				resource := &descriptor.Resource{}
				if normalization != "" {
					value := digest.FromString("manifest").Encoded()
					if mediaType == "application/octet-stream" && normalization == internaldigest.GenericBlobDigestV1 {
						value = digest.FromString("archive").Encoded()
					}
					resource.Digest = &descriptor.Digest{
						HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
						NormalisationAlgorithm: normalization,
						Value:                  value,
					}
				}
				original := resource.Digest
				var snapshot descriptor.Digest
				if original != nil {
					snapshot = *original
				}
				ab, err := ociblob.NewArtifactBlobWithMediaType(resource, direct.NewFromBytes([]byte("archive")), mediaType)
				r.NoError(err)
				buffered, err := ab.Buffer()
				r.NoError(err)
				mt, ok := buffered.MediaType()
				r.True(ok)
				r.Equal(mediaType, mt)
				dig, ok := buffered.Digest()
				r.True(ok)
				r.Equal(digest.FromString("archive").String(), dig)
				r.Equal(int64(len("archive")), buffered.Size())
				var content bytes.Buffer
				r.NoError(blob.Copy(&content, buffered))
				r.Equal("archive", content.String())
				if original != nil {
					r.Same(original, resource.Digest)
					r.Equal(snapshot, *resource.Digest)
				} else {
					r.Nil(resource.Digest)
				}
			})
		}
	}
}

func TestArtifactBlob_ConcurrentPrecalculatedDigest(t *testing.T) {
	r := require.New(t)
	resource := &descriptor.Resource{Digest: &descriptor.Digest{
		HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
		NormalisationAlgorithm: internaldigest.OCIArtifactDigestV1,
		Value:                  digest.FromString("manifest").Encoded(),
	}}
	original := *resource.Digest
	ab, err := ociblob.NewArtifactBlob(resource, &mockBlob{})
	r.NoError(err)
	checksum := digest.FromString("archive").String()
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for range 100 {
				ab.SetPrecalculatedDigest(checksum)
				dig, ok := ab.Digest()
				assert.True(t, ok)
				assert.Equal(t, checksum, dig)
				assert.True(t, ab.HasPrecalculatedDigest())
				assert.Equal(t, original, *resource.Digest)
			}
		})
	}
	wg.Wait()
	r.Equal(original, *resource.Digest)
	ab.SetPrecalculatedDigest("")
	r.False(ab.HasPrecalculatedDigest())
}

func localBlobAccess(t *testing.T, representation, mediaType string) runtime.Typed {
	t.Helper()
	r := require.New(t)
	local := &v2.LocalBlob{
		Type:      runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
		MediaType: mediaType,
	}
	switch representation {
	case "runtime access":
		return &descriptor.LocalBlob{MediaType: mediaType}
	case "v2 access":
		return local
	case "raw access":
		data, err := json.Marshal(local)
		r.NoError(err)
		return &runtime.Raw{Type: local.Type, Data: data}
	case "unstructured access":
		access := runtime.NewUnstructured()
		access.Data["type"] = local.Type.String()
		access.Data["mediaType"] = mediaType
		return &access
	default:
		t.Fatalf("unknown access representation %q", representation)
		return nil
	}
}

func TestArtifactBlob_HasPrecalculatedDigestDoesNotRead(t *testing.T) {
	for _, kind := range []string{"source", "layout", "generic fallback", "non-generic"} {
		for _, precalculated := range []bool{false, true} {
			t.Run(kind+"/"+map[bool]string{false: "unknown", true: "known"}[precalculated], func(t *testing.T) {
				r := require.New(t)
				checksum := digest.FromString("archive").String()
				var artifact descriptor.Artifact = &descriptor.Source{}
				mediaType := "application/octet-stream"
				if kind != "source" {
					normalization := internaldigest.GenericBlobDigestV1
					if kind == "non-generic" {
						normalization = internaldigest.OCIArtifactDigestV1
					}
					artifact = &descriptor.Resource{Digest: &descriptor.Digest{
						HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
						NormalisationAlgorithm: normalization,
						Value:                  digest.FromString("archive").Encoded(),
					}}
					if kind == "layout" {
						mediaType = layout.MediaTypeOCIImageLayoutTarV1
					}
				}
				ab, err := ociblob.NewArtifactBlobWithMediaType(artifact, &mockBlob{}, mediaType)
				r.NoError(err)
				reader := &countingReader{Reader: bytes.NewBufferString("archive")}
				lazy := inmemory.New(reader)
				if precalculated {
					lazy.SetPrecalculatedDigest(checksum)
				}
				ab.ReadOnlyBlob = lazy
				for range 3 {
					r.Equal(precalculated || kind == "generic fallback", ab.HasPrecalculatedDigest())
				}
				r.Zero(reader.reads)
				// A DigestAware interface alone offers no guarantee of a cheap query.
				ab.ReadOnlyBlob = &digestOnlyBlob{ReadOnlyBlob: lazy, DigestAware: lazy}
				r.Equal(kind == "generic fallback", ab.HasPrecalculatedDigest())
				r.Zero(reader.reads)
				ab.SetPrecalculatedDigest(checksum)
				r.True(ab.HasPrecalculatedDigest())
				r.Zero(reader.reads)
			})
		}
	}
}

type countingReader struct {
	io.Reader
	reads int
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

type digestOnlyBlob struct {
	blob.ReadOnlyBlob
	blob.DigestAware
}

type mockDigestAwareMediaBlob struct {
	*direct.Blob
	checksum string
}

func (b *mockDigestAwareMediaBlob) Digest() (string, bool) {
	return b.checksum, b.checksum != ""
}

func (b *mockDigestAwareMediaBlob) HasPrecalculatedDigest() bool {
	return b.checksum != ""
}

func (b *mockDigestAwareMediaBlob) SetPrecalculatedDigest(dig string) {
	b.checksum = dig
}

// Helper types for testing
type mockSizeAwareBlob struct {
	blob.ReadOnlyBlob
	size int64
}

func (m *mockSizeAwareBlob) Size() int64 {
	return m.size
}

type mockDigestAwareBlob struct {
	blob.ReadOnlyBlob
	digest string
}

func (m *mockDigestAwareBlob) Digest() (string, bool) {
	return m.digest, m.digest != ""
}

func (m *mockDigestAwareBlob) HasPrecalculatedDigest() bool {
	return m.digest != ""
}

func (m *mockDigestAwareBlob) SetPrecalculatedDigest(dig string) {
	m.digest = dig
}

type mockMediaTypeAwareBlob struct {
	blob.ReadOnlyBlob
	mediaType string
}

func (m *mockMediaTypeAwareBlob) MediaType() (string, bool) {
	return m.mediaType, m.mediaType != ""
}
