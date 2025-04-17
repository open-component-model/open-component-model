package file

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		compressedBlob := NewCompressedBlob(baseBlob)

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
	})

	t.Run("error from base blob", func(t *testing.T) {
		// Create a blob that returns an error
		expectedErr := errors.New("test error")
		baseBlob := &testBlob{err: expectedErr}

		// Create compressed blob
		compressedBlob := NewCompressedBlob(baseBlob)

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
		compressedBlob := NewCompressedBlob(baseBlob)

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
}
