package tar

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOCILayoutTarWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	writer, err := NewOCILayoutWriterWithTempFile(buf, t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, writer)
	require.NoError(t, writer.Close())
}

func TestOCILayoutTarWriter_Push(t *testing.T) {
	tests := []struct {
		name        string
		desc        ociImageSpecV1.Descriptor
		data        []byte
		expectError bool
	}{
		{
			name: "valid manifest",
			desc: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    digest.FromString("test content"),
				Size:      12,
			},
			data:        []byte("test content"),
			expectError: false,
		},
		{
			name: "valid blob",
			desc: ociImageSpecV1.Descriptor{
				MediaType: "application/octet-stream",
				Digest:    digest.FromString("test blob"),
				Size:      9,
			},
			data:        []byte("test blob"),
			expectError: false,
		},
		{
			name: "invalid digest",
			desc: ociImageSpecV1.Descriptor{
				MediaType: "application/octet-stream",
				Digest:    "invalid:digest",
				Size:      9,
			},
			data:        []byte("test blob"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			writer, err := NewOCILayoutWriterWithTempFile(buf, t.TempDir())
			require.NoError(t, err)
			defer writer.Close()

			err = writer.Push(context.Background(), tt.desc, bytes.NewReader(tt.data))
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestOCILayoutTarWriter_Tag(t *testing.T) {
	tests := []struct {
		name        string
		desc        ociImageSpecV1.Descriptor
		reference   string
		expectError bool
	}{
		{
			name: "valid tag",
			desc: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    digest.FromString("test content"),
				Size:      12,
			},
			reference:   "test-tag",
			expectError: false,
		},
		{
			name: "empty reference",
			desc: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    digest.FromString("test content"),
				Size:      12,
			},
			reference:   "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			writer, err := NewOCILayoutWriterWithTempFile(buf, t.TempDir())
			require.NoError(t, err)
			defer writer.Close()

			// First push the content
			err = writer.Push(context.Background(), tt.desc, bytes.NewReader([]byte("test content")))
			require.NoError(t, err)

			// Then try to tag it
			err = writer.Tag(context.Background(), tt.desc, tt.reference)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Verify the tag was set correctly
			resolved, err := writer.Resolve(context.Background(), tt.reference)
			assert.NoError(t, err)
			assert.Equal(t, tt.desc.Digest, resolved.Digest)
		})
	}
}

func TestOCILayoutTarWriter_Close(t *testing.T) {
	buf := &bytes.Buffer{}
	writer, err := NewOCILayoutWriterWithTempFile(buf, t.TempDir())
	require.NoError(t, err)

	// Push some content
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromString("test content"),
		Size:      12,
	}
	err = writer.Push(context.Background(), desc, bytes.NewReader([]byte("test content")))
	require.NoError(t, err)

	// Close the writer
	err = writer.Close()
	require.NoError(t, err)

	// Verify the index.json and oci-layout files were written
	tarReader := tar.NewReader(buf)
	files := make(map[string][]byte)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		content, err := io.ReadAll(tarReader)
		require.NoError(t, err)
		files[header.Name] = content
	}

	// Verify index.json
	indexJSON, ok := files[ociImageSpecV1.ImageIndexFile]
	require.True(t, ok)
	var index ociImageSpecV1.Index
	err = json.Unmarshal(indexJSON, &index)
	require.NoError(t, err)
	assert.Equal(t, int(2), index.SchemaVersion)

	// Verify oci-layout
	layoutJSON, ok := files[ociImageSpecV1.ImageLayoutFile]
	require.True(t, ok)
	var layout ociImageSpecV1.ImageLayout
	err = json.Unmarshal(layoutJSON, &layout)
	require.NoError(t, err)
	assert.Equal(t, ociImageSpecV1.ImageLayoutVersion, layout.Version)
}

func TestOCILayoutTarWriter_Exists(t *testing.T) {
	buf := &bytes.Buffer{}
	writer, err := NewOCILayoutWriterWithTempFile(buf, t.TempDir())
	require.NoError(t, err)
	defer writer.Close()

	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromString("test content"),
		Size:      12,
	}

	// Check existence before pushing
	exists, err := writer.Exists(context.Background(), desc)
	require.NoError(t, err)
	assert.False(t, exists)

	// Push the content
	err = writer.Push(context.Background(), desc, bytes.NewReader([]byte("test content")))
	require.NoError(t, err)

	// Check existence after pushing
	exists, err = writer.Exists(context.Background(), desc)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestOCILayoutTarWriter_Fetch(t *testing.T) {
	buf := &bytes.Buffer{}
	writer, err := NewOCILayoutWriterWithTempFile(buf, t.TempDir())
	require.NoError(t, err)
	defer writer.Close()

	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromString("test content"),
		Size:      12,
	}

	// Fetch should always return ErrUnsupported
	reader, err := writer.Fetch(context.Background(), desc)
	assert.Error(t, err)
	assert.Nil(t, reader)
}

func TestOCILayoutTarWriter_Resolve(t *testing.T) {
	tests := []struct {
		name        string
		desc        ociImageSpecV1.Descriptor
		reference   string
		expectError bool
	}{
		{
			name: "existing reference",
			desc: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    digest.FromString("test content"),
				Size:      12,
			},
			reference:   "test-tag",
			expectError: false,
		},
		{
			name: "non-existent reference",
			desc: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    digest.FromString("test content"),
				Size:      12,
			},
			reference:   "non-existent",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			writer, err := NewOCILayoutWriterWithTempFile(buf, t.TempDir())
			require.NoError(t, err)
			defer writer.Close()

			// Push and tag the content if we expect it to exist
			if !tt.expectError {
				err := writer.Push(context.Background(), tt.desc, bytes.NewReader([]byte("test content")))
				require.NoError(t, err)
				err = writer.Tag(context.Background(), tt.desc, tt.reference)
				require.NoError(t, err)
			}

			// Try to resolve the reference
			resolved, err := writer.Resolve(context.Background(), tt.reference)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.desc.Digest, resolved.Digest)
		})
	}
}

func TestBlobPath(t *testing.T) {
	tests := []struct {
		name        string
		digest      digest.Digest
		expectError bool
	}{
		{
			name:        "valid digest",
			digest:      digest.FromString("test content"),
			expectError: false,
		},
		{
			name:        "invalid digest",
			digest:      "invalid:digest",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := blobPath(tt.digest)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			expectedPath := "blobs/sha256/" + tt.digest.Encoded()
			assert.Equal(t, expectedPath, path)
		})
	}
}

func TestMemoryResolver(t *testing.T) {
	resolver := newMemoryResolver()
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromString("test content"),
		Size:      12,
	}

	// Test Tag
	err := resolver.Tag(context.Background(), desc, "test-tag")
	require.NoError(t, err)

	// Test Resolve
	resolved, err := resolver.Resolve(context.Background(), "test-tag")
	require.NoError(t, err)
	assert.Equal(t, desc.Digest, resolved.Digest)

	// Test Map
	refMap := resolver.Map()
	require.Len(t, refMap, 1)
	assert.Equal(t, desc.Digest, refMap["test-tag"].Digest)

	// Test TagSet
	tagSet := resolver.TagSet(desc)
	assert.True(t, tagSet.Contains("test-tag"))

	// Test Untag
	resolver.Untag("test-tag")
	resolved, err = resolver.Resolve(context.Background(), "test-tag")
	assert.Error(t, err)
}

func TestDeleteAnnotationRefName(t *testing.T) {
	tests := []struct {
		name     string
		desc     ociImageSpecV1.Descriptor
		expected ociImageSpecV1.Descriptor
	}{
		{
			name: "with ref name annotation",
			desc: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					ociImageSpecV1.AnnotationRefName: "test-tag",
					"other":                          "value",
				},
			},
			expected: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					"other": "value",
				},
			},
		},
		{
			name: "without ref name annotation",
			desc: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					"other": "value",
				},
			},
			expected: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					"other": "value",
				},
			},
		},
		{
			name: "only ref name annotation",
			desc: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					ociImageSpecV1.AnnotationRefName: "test-tag",
				},
			},
			expected: ociImageSpecV1.Descriptor{
				Annotations: nil,
			},
		},
		{
			name: "nil annotations",
			desc: ociImageSpecV1.Descriptor{
				Annotations: nil,
			},
			expected: ociImageSpecV1.Descriptor{
				Annotations: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deleteAnnotationRefName(tt.desc)
			assert.Equal(t, tt.expected.Annotations, result.Annotations)
		})
	}
}

func TestOCILayoutTarWriter_ConcurrentPush(t *testing.T) {
	buf := &bytes.Buffer{}
	writer, err := NewOCILayoutWriterWithTempFile(buf, t.TempDir())
	require.NoError(t, err)

	const numBlobs = 20
	type blobEntry struct {
		desc ociImageSpecV1.Descriptor
		data []byte
	}

	blobs := make([]blobEntry, numBlobs)
	for i := range numBlobs {
		data := []byte(fmt.Sprintf("blob content number %d with some padding to make it interesting", i))
		blobs[i] = blobEntry{
			desc: ociImageSpecV1.Descriptor{
				MediaType: "application/octet-stream",
				Digest:    digest.FromBytes(data),
				Size:      int64(len(data)),
			},
			data: data,
		}
	}

	// Push all blobs concurrently
	var wg sync.WaitGroup
	errs := make([]error, numBlobs)
	for i := range numBlobs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = writer.Push(context.Background(), blobs[idx].desc, bytes.NewReader(blobs[idx].data))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "Push failed for blob %d", i)
	}

	// Close to finalize the tar
	require.NoError(t, writer.Close())

	// Read back the tar and verify all blobs are present
	tarReader := tar.NewReader(buf)
	foundBlobs := make(map[string][]byte)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		content, err := io.ReadAll(tarReader)
		require.NoError(t, err)
		foundBlobs[header.Name] = content
	}

	// Verify all blobs are in the tar
	for i, b := range blobs {
		bPath := "blobs/" + b.desc.Digest.Algorithm().String() + "/" + b.desc.Digest.Encoded()
		data, ok := foundBlobs[bPath]
		require.True(t, ok, "blob %d not found in tar at path %s", i, bPath)
		assert.Equal(t, b.data, data, "blob %d content mismatch", i)
	}

	// Verify index.json and oci-layout are present
	_, ok := foundBlobs[ociImageSpecV1.ImageIndexFile]
	assert.True(t, ok, "index.json not found in tar")
	_, ok = foundBlobs[ociImageSpecV1.ImageLayoutFile]
	assert.True(t, ok, "oci-layout not found in tar")
}

func TestOCILayoutTarWriter_ScratchClosedOnClose(t *testing.T) {
	// When scratch implements io.Closer (like *os.File), Close() calls it.
	tmpFile, err := os.CreateTemp("", "oci-layout-test-*")
	require.NoError(t, err)
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	buf := &bytes.Buffer{}
	writer := NewOCILayoutWriter(buf, tmpFile)
	require.NoError(t, writer.Close())

	// The file should still exist on disk (Close closes the fd, doesn't remove)
	_, err = os.Stat(tmpPath)
	require.NoError(t, err, "file should still exist, only fd is closed")
}

func TestOCILayoutWriterWithTempFile_RemovesOnClose(t *testing.T) {
	buf := &bytes.Buffer{}
	writer, err := NewOCILayoutWriterWithTempFile(buf, t.TempDir())
	require.NoError(t, err)

	// Peek at the underlying file path via the removingCloser
	rc := writer.buf.(*removingCloser)
	tmpPath := rc.File.Name()

	_, err = os.Stat(tmpPath)
	require.NoError(t, err, "temp file should exist before Close")

	require.NoError(t, writer.Close())

	// removingCloser should have both closed and removed the file
	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), "temp file should be removed after Close")
}

func TestOCILayoutWriter_DirectConstructor(t *testing.T) {
	// Verify the direct constructor works with a caller-managed temp file
	tmpFile, err := os.CreateTemp("", "oci-layout-test-*")
	require.NoError(t, err)
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()

	buf := &bytes.Buffer{}
	writer := NewOCILayoutWriter(buf, tmpFile)

	desc := ociImageSpecV1.Descriptor{
		MediaType: "application/octet-stream",
		Digest:    digest.FromString("direct test"),
		Size:      int64(len("direct test")),
	}
	require.NoError(t, writer.Push(context.Background(), desc, bytes.NewReader([]byte("direct test"))))
	require.NoError(t, writer.Close())

	// Verify the tar output is valid
	tarReader := tar.NewReader(buf)
	files := make(map[string][]byte)
	for {
		header, readErr := tarReader.Next()
		if readErr == io.EOF {
			break
		}
		require.NoError(t, readErr)
		content, readErr := io.ReadAll(tarReader)
		require.NoError(t, readErr)
		files[header.Name] = content
	}

	bPath := "blobs/" + desc.Digest.Algorithm().String() + "/" + desc.Digest.Encoded()
	assert.Equal(t, []byte("direct test"), files[bPath])
	assert.Contains(t, files, ociImageSpecV1.ImageIndexFile)
	assert.Contains(t, files, ociImageSpecV1.ImageLayoutFile)
}

// makeBenchBlobs pre-generates n blobs of the given size with valid digests.
func makeBenchBlobs(n int, size int) []struct {
	desc ociImageSpecV1.Descriptor
	data []byte
} {
	blobs := make([]struct {
		desc ociImageSpecV1.Descriptor
		data []byte
	}, n)
	for i := range n {
		data := make([]byte, size)
		// Fill with a recognizable pattern per blob
		for j := range data {
			data[j] = byte(i + j)
		}
		blobs[i].data = data
		blobs[i].desc = ociImageSpecV1.Descriptor{
			MediaType: "application/octet-stream",
			Digest:    digest.FromBytes(data),
			Size:      int64(size),
		}
	}
	return blobs
}

func BenchmarkOCILayoutWriter_Push_Sequential(b *testing.B) {
	for _, blobSize := range []int{1024, 64 * 1024, 1024 * 1024} {
		b.Run(fmt.Sprintf("blob_%dKB", blobSize/1024), func(b *testing.B) {
			const numBlobs = 50
			blobs := makeBenchBlobs(numBlobs, blobSize)

			b.SetBytes(int64(numBlobs * blobSize))
			b.ResetTimer()

			for range b.N {
				tmpFile, err := os.CreateTemp("", "bench-seq-*")
				if err != nil {
					b.Fatal(err)
				}
				writer := NewOCILayoutWriter(io.Discard, tmpFile)

				for j := range numBlobs {
					if err := writer.Push(context.Background(), blobs[j].desc, bytes.NewReader(blobs[j].data)); err != nil {
						b.Fatal(err)
					}
				}
				if err := writer.Close(); err != nil {
					b.Fatal(err)
				}
				os.Remove(tmpFile.Name())
			}
		})
	}
}

func BenchmarkOCILayoutWriter_Push_Concurrent(b *testing.B) {
	for _, blobSize := range []int{1024, 64 * 1024, 1024 * 1024} {
		b.Run(fmt.Sprintf("blob_%dKB", blobSize/1024), func(b *testing.B) {
			const numBlobs = 50
			blobs := makeBenchBlobs(numBlobs, blobSize)

			b.SetBytes(int64(numBlobs * blobSize))
			b.ResetTimer()

			for range b.N {
				tmpFile, err := os.CreateTemp(b.TempDir(), "bench-conc-*")
				if err != nil {
					b.Fatal(err)
				}
				writer := NewOCILayoutWriter(io.Discard, tmpFile)

				var (
					wg       sync.WaitGroup
					mu       sync.Mutex
					firstErr error
				)
				for j := range numBlobs {
					wg.Add(1)
					go func(idx int) {
						defer wg.Done()
						if err := writer.Push(b.Context(), blobs[idx].desc, bytes.NewReader(blobs[idx].data)); err != nil {
							mu.Lock()
							if firstErr == nil {
								firstErr = err
							}
							mu.Unlock()
						}
					}(j)
				}
				wg.Wait()
				if firstErr != nil {
					b.Fatal(firstErr)
				}
				if err := writer.Close(); err != nil {
					b.Fatal(err)
				}
				if err := os.Remove(tmpFile.Name()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
