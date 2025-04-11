package tar

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
