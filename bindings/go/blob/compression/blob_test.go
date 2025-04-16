package compression_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
)

type testBlob struct {
	data []byte
	err  error
}

func (b *testBlob) ReadCloser() (io.ReadCloser, error) {
	if b.err != nil {
		return nil, b.err
	}
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func TestCompressedBlob(t *testing.T) {
	t.Run("successful compression and decompression", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)
		// Create test data
		testData := []byte("Hello, this is a test string for compression!")

		// Create a simple read-only blob
		baseBlob := &testBlob{data: testData}

		// Create compressed blob
		compressedBlob := compression.Compress(baseBlob)

		// GetFor the compressed reader
		rc, err := compressedBlob.ReadCloser()
		r.NoError(err)
		t.Cleanup(func() { r.NoError(rc.Close()) })

		// Read and decompress the data
		gzReader, err := gzip.NewReader(rc)
		r.NoError(err)
		t.Cleanup(func() { r.NoError(gzReader.Close()) })

		// Read the decompressed data
		decompressedData, err := io.ReadAll(gzReader)
		r.NoError(err)

		// Verify the decompressed data matches the original
		a.Equal(testData, decompressedData)

		// Verify media type
		mediaType, known := compressedBlob.MediaType()
		a.True(known)
		a.Equal("application/gzip", mediaType)

		t.Run("decompressed blob", func(t *testing.T) {
			a := assert.New(t)
			decompressedBlob, err := compression.Decompress(compressedBlob)
			a.NoError(err)
			a.IsType(&compression.DecompressedBlob{}, decompressedBlob)
			mediaType, ok := decompressedBlob.(blob.MediaTypeAware).MediaType()
			a.True(ok)
			a.Equal("application/octet-stream", mediaType)

			drc, err := decompressedBlob.ReadCloser()
			a.NoError(err)
			t.Cleanup(func() { r.NoError(drc.Close()) })
			decompressedData, err = io.ReadAll(drc)
			r.NoError(err)
			r.Equal(testData, decompressedData)
		})
	})

	t.Run("error from base blob", func(t *testing.T) {
		// Create a blob that returns an error
		expectedErr := errors.New("test error")
		baseBlob := &testBlob{err: expectedErr}

		// Create compressed blob
		compressedBlob := compression.Compress(baseBlob)

		// Attempt to get reader
		rc, err := compressedBlob.ReadCloser()
		assert.ErrorIs(t, err, expectedErr)
		assert.Nil(t, rc)
	})

	t.Run("empty blob", func(t *testing.T) {
		r := require.New(t)
		// Create an empty blob
		baseBlob := &testBlob{data: []byte{}}

		// Create compressed blob
		compressedBlob := compression.Compress(baseBlob)

		// GetFor the compressed reader
		rc, err := compressedBlob.ReadCloser()
		r.NoError(err)
		t.Cleanup(func() { r.NoError(rc.Close()) })

		// Read and decompress the data
		gzReader, err := gzip.NewReader(rc)
		r.NoError(err)
		t.Cleanup(func() { r.NoError(gzReader.Close()) })

		// Read the decompressed data
		decompressedData, err := io.ReadAll(gzReader)
		r.NoError(err)

		// Verify empty data
		assert.Empty(t, decompressedData)
	})

	t.Run("media type handling", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)

		// Test with custom media type
		baseBlob := &testBlob{data: []byte("test")}
		compressedBlob := compression.Compress(baseBlob)

		// Test media type for compressed blob
		mediaType, known := compressedBlob.MediaType()
		a.True(known)
		a.Equal("application/gzip", mediaType)

		// Test decompression with custom media type
		decompressedBlob, err := compression.Decompress(compressedBlob)
		r.NoError(err)
		mediaType, known = decompressedBlob.(blob.MediaTypeAware).MediaType()
		a.True(known)
		a.Equal("application/octet-stream", mediaType)
	})

	t.Run("decompression error handling", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)

		// Test with invalid gzip data
		nogzip := []byte("not a valid gzip stream")
		baseBlob := &testBlob{data: nogzip}
		compressedBlob := compression.Compress(baseBlob)

		// Try to decompress
		decompressedBlob, err := compression.Decompress(compressedBlob)
		r.NoError(err) // Decompression should succeed initially

		// Reading should be transparent
		rc, err := decompressedBlob.ReadCloser()
		r.NoError(err)
		t.Cleanup(func() { r.NoError(rc.Close()) })

		data, err := io.ReadAll(rc)
		a.NoError(err)
		a.Equal(nogzip, data)
	})

	t.Run("decompress non-compressed blob", func(t *testing.T) {
		r := require.New(t)
		a := assert.New(t)

		// Create a non-compressed blob
		baseBlob := &testBlob{data: []byte("test")}

		// Try to decompress
		decompressedBlob, err := compression.Decompress(baseBlob)
		r.NoError(err)

		// Should return the original blob
		a.Equal(baseBlob, decompressedBlob)
	})
}
