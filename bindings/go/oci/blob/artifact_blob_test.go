package blob_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "crypto/sha512" // SHA-512 precalculated checksum hints

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
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
			require.NoError(t, err)
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
		name        string
		resource    *descriptor.Resource
		newDigest   string
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

			original, snapshot := tt.resource.Digest, tt.resource.Digest.DeepCopy()
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
			require.Same(t, original, tt.resource.Digest)
			require.Equal(t, snapshot, tt.resource.Digest)
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
	assert.Equal(t, newDigest, desc.Digest)
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
		{
			name: "matching SHA-512 digests",
			resourceDigest: &descriptor.Digest{
				HashAlgorithm: internaldigest.HashAlgorithmSHA512,
				Value:         "ee26b0dd4af7e749aa1a8ee3c10ae9923f618980772e473f8819a5d4940e0db27ac185f8a0e1d5f84f88bc887fd67b143732c304cc5fa9ad8e6f57f50028a8ff",
			},
			blobDigest:    "sha512:ee26b0dd4af7e749aa1a8ee3c10ae9923f618980772e473f8819a5d4940e0db27ac185f8a0e1d5f84f88bc887fd67b143732c304cc5fa9ad8e6f57f50028a8ff",
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

// TestArtifactBlob_LayoutDigestSeparation checks that a layout archive's checksum is
// never compared with, or defaulted into, the resource digest naming its manifest.
// The classification is frozen at construction, survives Buffer and rewrapping, and
// needs no read of the blob.
func TestArtifactBlob_LayoutDigestSeparation(t *testing.T) {
	checksum := digest.FromString("archive").String()
	for _, normalization := range []string{"none", internaldigest.OCIArtifactDigestV1, internaldigest.GenericBlobDigestV1, ""} {
		for _, mediaType := range []string{layout.MediaTypeOCIImageLayoutTarV1, layout.MediaTypeOCIImageLayoutTarGzipV1} {
			for _, location := range []string{"explicit", "underlying", "runtime access", "v2 access", "raw access", "unstructured access"} {
				for _, known := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/known=%t", normalization, mediaType, location, known), func(t *testing.T) {
						r := require.New(t)
						resource := &descriptor.Resource{}
						if normalization != "none" {
							resource.Digest = &descriptor.Digest{
								HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
								NormalisationAlgorithm: normalization,
								Value:                  digest.FromString("manifest").Encoded(),
							}
						}
						original, snapshot := resource.Digest, resource.Digest.DeepCopy()
						explicitType, underlyingType := "application/octet-stream", "application/octet-stream"
						switch location {
						case "explicit":
							explicitType = mediaType
						case "underlying":
							underlyingType = mediaType
						default:
							resource.Access = localBlobAccess(t, location, mediaType)
						}
						reader := &countingReader{Reader: strings.NewReader("archive")}
						base := direct.New(reader, direct.WithMediaType(underlyingType))
						var b blob.ReadOnlyBlob = base
						wantDigest := ""
						if known {
							b, wantDigest = &mockDigestAwareBlob{ReadOnlyBlob: base, digest: checksum, mediaType: underlyingType}, checksum
						}
						if location != "explicit" {
							r.NoError(ociblob.UpdateArtifactWithInformationFromBlob(resource, b))
						}
						ab, err := ociblob.NewArtifactBlobWithMediaType(resource, b, explicitType)
						r.NoError(err)
						r.Zero(reader.reads, "classifying a layout must not read it")
						// Packing replaces the archive access with the root manifest access.
						for _, access := range []runtime.Typed{resource.Access, &v2.LocalBlob{MediaType: ociImageSpecV1.MediaTypeImageManifest}} {
							resource.Access = access
							requireChecksum(r, ab, wantDigest)
							r.Equal(known, ab.HasPrecalculatedDigest())
						}
						buffered, err := ab.Buffer()
						r.NoError(err)
						mt, _ := buffered.MediaType()
						r.Equal(explicitType, mt)
						r.NoError(ociblob.UpdateArtifactWithInformationFromBlob(resource, buffered))
						rewrapped, err := ociblob.NewArtifactBlob(resource, buffered)
						r.NoError(err)
						requireChecksum(r, buffered, checksum)
						requireChecksum(r, rewrapped, checksum)
						// Without the cached checksum, the manifest digest must not stand in.
						buffered.ReadOnlyBlob = &mockBlob{}
						requireChecksum(r, buffered, "")
						r.False(buffered.HasPrecalculatedDigest())
						r.Same(original, resource.Digest)
						r.Equal(snapshot, resource.Digest)
					})
				}
			}
		}
	}
}

func TestArtifactBlob_OrdinaryDigestNormalization(t *testing.T) {
	for _, normalization := range []string{"none", "", internaldigest.GenericBlobDigestV1, internaldigest.OCIArtifactDigestV1, "custom/v1"} {
		for _, known := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/known=%t", normalization, known), func(t *testing.T) {
				r := require.New(t)
				resource := &descriptor.Resource{}
				if normalization != "none" {
					resource.Digest = &descriptor.Digest{
						HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
						NormalisationAlgorithm: normalization,
						Value:                  digest.FromString("normalized").Encoded(),
					}
				}
				base := &mockDigestAwareBlob{}
				if known {
					base.digest = digest.FromString("bytes").String()
				}
				r.NoError(ociblob.UpdateArtifactWithInformationFromBlob(resource, base))
				ab, err := ociblob.NewArtifactBlob(resource, base)
				generic := normalization == "" || normalization == internaldigest.GenericBlobDigestV1
				if generic && known {
					r.ErrorContains(err, "resource blob digest mismatch")
					return
				}
				r.NoError(err)
				want := base.digest
				if !known && generic {
					want = digest.FromString("normalized").String()
				}
				requireChecksum(r, ab, want)
				r.Equal(want != "", ab.HasPrecalculatedDigest())
				if normalization == "none" && known {
					r.Equal(&descriptor.Digest{
						HashAlgorithm:          internaldigest.HashAlgorithmSHA256,
						NormalisationAlgorithm: internaldigest.GenericBlobDigestV1,
						Value:                  digest.FromString("bytes").Encoded(),
					}, resource.Digest, "an ordinary blob's checksum defaults a missing digest")
				} else if normalization == "none" {
					r.Nil(resource.Digest)
				}
			})
		}
	}
}

// TestArtifactBlob_Buffer checks that Buffer verifies an ordinary resource's expected
// checksum against the bytes read, even if the file changed after construction or a
// different precalculated checksum was set, and never rewrites the resource digest.
func TestArtifactBlob_Buffer(t *testing.T) {
	original, replacement := []byte("content A"), []byte("content B")
	for _, tc := range []struct {
		name, normalization string
		mutate              bool
		hint                digest.Algorithm
	}{
		{name: "generic", normalization: internaldigest.GenericBlobDigestV1},
		{name: "unlabelled"},
		{name: "OCI label is not a byte checksum", normalization: internaldigest.OCIArtifactDigestV1},
		{name: "defaulted", normalization: "none"},
		{name: "generic/sha512 hint", normalization: internaldigest.GenericBlobDigestV1, hint: digest.SHA512},
		{name: "generic/replaced", normalization: internaldigest.GenericBlobDigestV1, mutate: true},
		{name: "generic/replaced with sha256 hint", normalization: internaldigest.GenericBlobDigestV1, mutate: true, hint: digest.SHA256},
		{name: "generic/replaced with sha512 hint", normalization: internaldigest.GenericBlobDigestV1, mutate: true, hint: digest.SHA512},
		{name: "unlabelled/replaced", mutate: true},
		{name: "unlabelled/replaced with sha256 hint", mutate: true, hint: digest.SHA256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			path := filepath.Join(dir, "resource.bin")
			r.NoError(os.WriteFile(path, original, 0o600))
			resource := &descriptor.Resource{}
			if tc.normalization != "none" {
				value := digest.FromBytes(original).Encoded()
				if tc.normalization == internaldigest.OCIArtifactDigestV1 {
					value = digest.FromString("manifest").Encoded()
				}
				resource.Digest = &descriptor.Digest{
					HashAlgorithm: internaldigest.HashAlgorithmSHA256, NormalisationAlgorithm: tc.normalization, Value: value,
				}
			}
			ab, err := ociblob.NewArtifactBlobWithMediaType(resource, filesystem.NewFileBlob(os.DirFS(dir), "resource.bin"), "application/octet-stream")
			r.NoError(err)
			r.NotNil(resource.Digest, "an ordinary file's checksum defaults a missing digest")
			pointer, snapshot := resource.Digest, *resource.Digest
			data := original
			if tc.mutate {
				data = replacement
				r.NoError(os.WriteFile(path, data, 0o600))
			}
			if tc.hint != "" {
				ab.SetPrecalculatedDigest(tc.hint.FromBytes(data).String())
			}

			buffered, err := ab.Buffer()
			r.Same(pointer, resource.Digest)
			r.Equal(snapshot, *resource.Digest, "signed digest metadata must remain unchanged")
			if tc.mutate {
				r.Error(err, "the expected checksum, not the replacement's, must be verified")
				r.Nil(buffered)
				return
			}
			r.NoError(err)
			r.Same(ab.Artifact, buffered.Artifact)
			r.Equal(int64(len(original)), buffered.Size())
			requireChecksum(r, buffered, digest.FromBytes(original).String())
			mt, _ := buffered.MediaType()
			r.Equal("application/octet-stream", mt)
			// Removing the source proves subsequent reads use the eagerly loaded cache.
			r.NoError(os.Remove(path))
			var content bytes.Buffer
			r.NoError(blob.Copy(&content, buffered))
			r.Equal(original, content.Bytes())
		})
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
	sum := digest.FromString("archive")
	generic := &descriptor.Digest{HashAlgorithm: internaldigest.HashAlgorithmSHA256, NormalisationAlgorithm: internaldigest.GenericBlobDigestV1, Value: sum.Encoded()}
	nonGeneric := &descriptor.Digest{HashAlgorithm: internaldigest.HashAlgorithmSHA256, NormalisationAlgorithm: internaldigest.OCIArtifactDigestV1, Value: sum.Encoded()}
	for _, tc := range []struct {
		kind, mediaType string
		artifact        descriptor.Artifact
	}{
		{kind: "source", mediaType: "application/octet-stream", artifact: &descriptor.Source{}},
		{kind: "layout", mediaType: layout.MediaTypeOCIImageLayoutTarV1, artifact: &descriptor.Resource{Digest: generic}},
		{kind: "generic fallback", mediaType: "application/octet-stream", artifact: &descriptor.Resource{Digest: generic}},
		{kind: "non-generic", mediaType: "application/octet-stream", artifact: &descriptor.Resource{Digest: nonGeneric}},
	} {
		for _, precalculated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/precalculated=%t", tc.kind, precalculated), func(t *testing.T) {
				r := require.New(t)
				checksum := sum.String()
				ab, err := ociblob.NewArtifactBlobWithMediaType(tc.artifact, &mockBlob{}, tc.mediaType)
				r.NoError(err)
				reader := &countingReader{Reader: bytes.NewBufferString("archive")}
				lazy := inmemory.New(reader)
				if precalculated {
					lazy.SetPrecalculatedDigest(checksum)
				}
				ab.ReadOnlyBlob = lazy
				for range 3 {
					r.Equal(precalculated || tc.kind == "generic fallback", ab.HasPrecalculatedDigest())
				}
				if tc.kind == "generic fallback" { // a known resource checksum is not looked up by reading
					requireChecksum(r, ab, checksum)
				}
				r.Zero(reader.reads)
				// A DigestAware interface alone offers no guarantee of a cheap query.
				ab.ReadOnlyBlob = &digestOnlyBlob{ReadOnlyBlob: lazy, DigestAware: lazy}
				r.Equal(tc.kind == "generic fallback", ab.HasPrecalculatedDigest())
				r.Zero(reader.reads)
				ab.SetPrecalculatedDigest(checksum)
				r.True(ab.HasPrecalculatedDigest())
				r.Zero(reader.reads)
			})
		}
	}
}

// requireChecksum asserts the blob's byte checksum; an empty want means unknown.
func requireChecksum(r *require.Assertions, ab *ociblob.ArtifactBlob, want string) {
	dig, ok := ab.Digest()
	r.Equal(want, dig)
	r.Equal(want != "", ok)
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
	digest, mediaType string
}

func (m *mockDigestAwareBlob) MediaType() (string, bool) {
	return m.mediaType, m.mediaType != ""
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
