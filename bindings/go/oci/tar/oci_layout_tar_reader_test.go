package tar

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
)

func createTestOCILayout(t *testing.T, testBlobData []byte) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Create OCI layout file
	layoutContent := `{"imageLayoutVersion": "1.0.0"}`
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "oci-layout",
		Mode: 0644,
		Size: int64(len(layoutContent)),
	}))
	_, err := tw.Write([]byte(layoutContent))
	require.NoError(t, err)

	// Create blobs directory
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "blobs",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}))

	// Create sha256 directory
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     "blobs/sha256",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}))

	// Create a test blob
	blobDigest := digest.FromBytes(testBlobData)

	// Write test blob to blobs/sha256
	blobPath := "blobs/sha256/" + blobDigest.Encoded()
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: blobPath,
		Mode: 0644,
		Size: int64(len(testBlobData)),
	}))
	_, err = tw.Write(testBlobData)
	require.NoError(t, err)

	// Write empty index.json at root
	indexContent := `{"schemaVersion": 2, "manifests": []}`
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "index.json",
		Mode: 0644,
		Size: int64(len(indexContent)),
	}))
	_, err = tw.Write([]byte(indexContent))
	require.NoError(t, err)

	require.NoError(t, tw.Close())

	return buf.Bytes()
}

func createGzippedOCILayout(t *testing.T, data []byte) []byte {
	ociLayout := createTestOCILayout(t, data)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, err := gw.Write(ociLayout)
	require.NoError(t, err)
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

type testBlob struct {
	data []byte
}

func (b *testBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func (b *testBlob) Size() int64 {
	return int64(len(b.data))
}

func TestReadOCILayout(t *testing.T) {
	tests := []struct {
		name              string
		blob              blob.ReadOnlyBlob
		wantErr           bool
		expectInvalidBlob bool
	}{
		{
			name:    "valid OCI layout tarball",
			blob:    &testBlob{data: createTestOCILayout(t, []byte("test"))},
			wantErr: false,
		},
		{
			name:              "valid OCI layout tarball with other blob content",
			blob:              &testBlob{data: createTestOCILayout(t, []byte("other"))},
			expectInvalidBlob: true,
		},
		{
			name:    "valid gzipped OCI layout tarball",
			blob:    &testBlob{data: createGzippedOCILayout(t, []byte("test"))},
			wantErr: false,
		},
		{
			name:    "empty blob",
			blob:    &testBlob{data: []byte{}},
			wantErr: true,
		},
		{
			name:    "invalid tarball",
			blob:    &testBlob{data: []byte("not a tarball")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := ReadOCILayout(t.Context(), tt.blob)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			t.Cleanup(func() {
				assert.NoError(t, store.Close())
			})

			expected := []byte("test")
			// Verify we can read the test blob
			desc := v1.Descriptor{
				MediaType: "application/octet-stream",
				Digest:    digest.FromBytes([]byte("test")),
				Size:      int64(len(expected)),
			}

			if dataFromBlob, err := store.Fetch(t.Context(), desc); tt.expectInvalidBlob {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				data, err := io.ReadAll(dataFromBlob)
				assert.NoError(t, err)
				assert.Equal(t, data, expected)
			}
		})
	}
}

func TestReadOCILayout_SizeAware(t *testing.T) {
	// Test with a blob that's too small for gzip detection
	smallBlob := &testBlob{data: []byte{0x1F}} // Only one byte of gzip magic
	_, err := ReadOCILayout(context.Background(), smallBlob)
	assert.Error(t, err, "Expected error for blob too small for gzip detection")
}

func TestReadOCILayout_Close(t *testing.T) {
	data := createTestOCILayout(t, []byte("test"))
	store, err := ReadOCILayout(context.Background(), &testBlob{data: data})
	require.NoError(t, err)
	assert.NoError(t, store.Close())
}

// TestCloseableReadOnlyStore_MainArtifacts covers main-artifact selection: a
// manifest that declares a subject is a referrer (regardless of artifact type)
// and is excluded; the remaining candidates are reduced to the top-level set.
func TestCloseableReadOnlyStore_MainArtifacts(t *testing.T) {
	t.Run("subject-declaring manifests are excluded from main artifacts", func(t *testing.T) {
		var main v1.Descriptor
		store := readLayout(t, func(w *OCILayoutWriter) {
			main = pack(t, w, "main", "", nil)
			// Two referrers on main with different artifact types. Detection is by
			// subject, so both must be excluded.
			pack(t, w, "ownership-referrer", annotations.OwnershipArtifactType, &main)
			pack(t, w, "plain-referrer", "", &main)
		})

		assert.Equal(t, []string{main.Digest.String()}, digests(store.MainArtifacts(t.Context())),
			"the only main artifact is the subject-less manifest")
	})

	t.Run("no referrers: main artifact only", func(t *testing.T) {
		var main v1.Descriptor
		store := readLayout(t, func(w *OCILayoutWriter) {
			main = pack(t, w, "main", "", nil)
		})

		assert.Equal(t, []string{main.Digest.String()}, digests(store.MainArtifacts(t.Context())))
	})

	t.Run("main selection drops manifests contained by another", func(t *testing.T) {
		// An image index over two child manifests, plus a referrer on the index.
		// The children are contained by the index, so only the index is a main
		// artifact; the referrer is held aside.
		var index v1.Descriptor
		store := readLayout(t, func(w *OCILayoutWriter) {
			child1 := pack(t, w, "child1", "", nil)
			child2 := pack(t, w, "child2", "", nil)
			index = packIndex(t, w, child1, child2)
			pack(t, w, "index-referrer", "", &index)
		})

		assert.Equal(t, []string{index.Digest.String()}, digests(store.MainArtifacts(t.Context())),
			"only the top-level index is a main artifact; its children are contained")
	})
}

// readLayout builds an OCI layout via build, then reads it back into a store
// closed on test cleanup. pack and packIndex push every blob they reference, so
// build only has to call them.
func readLayout(t *testing.T, build func(w *OCILayoutWriter)) *CloseableReadOnlyStore {
	t.Helper()
	var buf bytes.Buffer
	w, err := NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	require.NoError(t, err)
	build(w)
	require.NoError(t, w.Close())
	store, err := ReadOCILayout(t.Context(), &testBlob{data: buf.Bytes()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

// pack builds and pushes a minimal image manifest (empty config and layer) and
// returns its descriptor. A non-nil subject makes it a referrer. name becomes
// the title annotation, which documents the manifest and keeps otherwise
// identical siblings distinct. An empty artifactType falls back to a generic
// type, which image-spec v1.1 requires alongside the empty config.
func pack(t *testing.T, w *OCILayoutWriter, name, artifactType string, subject *v1.Descriptor) v1.Descriptor {
	t.Helper()
	if artifactType == "" {
		artifactType = "application/vnd.test.artifact.v1+json"
	}
	desc, err := oras.PackManifest(t.Context(), w, oras.PackManifestVersion1_1, artifactType, oras.PackManifestOptions{
		Subject:             subject,
		ManifestAnnotations: map[string]string{v1.AnnotationTitle: name},
	})
	require.NoError(t, err)
	return desc
}

// packIndex builds and pushes an image index over the given children, returning
// its descriptor.
func packIndex(t *testing.T, w *OCILayoutWriter, children ...v1.Descriptor) v1.Descriptor {
	t.Helper()
	data, err := json.Marshal(v1.Index{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: v1.MediaTypeImageIndex,
		Manifests: children,
	})
	require.NoError(t, err)
	desc := content.NewDescriptorFromBytes(v1.MediaTypeImageIndex, data)
	require.NoError(t, w.Push(t.Context(), desc, bytes.NewReader(data)))
	return desc
}

// digests maps descriptors to their digest strings for order-independent compares.
func digests(descs []v1.Descriptor) []string {
	out := make([]string, len(descs))
	for i, d := range descs {
		out[i] = d.Digest.String()
	}
	return out
}
