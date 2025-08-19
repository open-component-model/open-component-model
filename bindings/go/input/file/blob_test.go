package file_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/input/file"
	v1 "ocm.software/open-component-model/bindings/go/input/file/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestInputFileBlob_MediaType(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		expected  string
		known     bool
	}{
		{
			name:      "with media type",
			mediaType: "text/plain",
			expected:  "text/plain",
			known:     true,
		},
		{
			name:      "empty media type",
			mediaType: "",
			expected:  "",
			known:     false,
		},
		{
			name:      "application/json media type",
			mediaType: "application/json",
			expected:  "application/json",
			known:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			// Create a temporary file for testing
			tempDir := t.TempDir()
			filePath := filepath.Join(tempDir, "test.txt")
			err := os.WriteFile(filePath, []byte("test data"), 0644)
			r.NoError(err)

			// Create filesystem blob
			fsBlob, err := filesystem.GetBlobFromOSPath(filePath)
			r.NoError(err)

			// Create InputFileBlob
			inputBlob := &file.InputFileBlob{
				Blob:          fsBlob,
				FileMediaType: tt.mediaType,
			}

			// Test MediaType method
			mediaType, known := inputBlob.MediaType()
			r.Equal(tt.expected, mediaType)
			r.Equal(tt.known, known)
		})
	}
}

func TestInputFileBlob_InterfaceCompliance(t *testing.T) {
	r := require.New(t)

	// Create a temporary file for testing
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test.txt")
	err := os.WriteFile(filePath, []byte("test data"), 0644)
	r.NoError(err)

	// Create filesystem blob
	fsBlob, err := filesystem.GetBlobFromOSPath(filePath)
	r.NoError(err)

	// Create InputFileBlob
	inputBlob := &file.InputFileBlob{
		Blob:          fsBlob,
		FileMediaType: "text/plain",
	}

	// Test interface compliance
	var _ blob.MediaTypeAware = inputBlob
	var _ blob.SizeAware = inputBlob
	var _ blob.DigestAware = inputBlob

	// Test that methods work correctly
	mediaType, known := inputBlob.MediaType()
	r.Equal("text/plain", mediaType)
	r.True(known)

	size := inputBlob.Size()
	r.Greater(size, int64(0))

	digest, ok := inputBlob.Digest()
	r.True(ok)
	r.NotEmpty(digest)

	reader, err := inputBlob.ReadCloser()
	r.NoError(err)
	defer func() {
		r.NoError(reader.Close())
	}()

	data, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal("test data", string(data))
}

func TestGetV1FileBlob_Success(t *testing.T) {
	tests := []struct {
		name       string
		fileData   string
		mediaType  string
		compress   bool
		expectGzip bool
	}{
		{
			name:       "text file without compression",
			fileData:   "Hello, World!",
			mediaType:  "text/plain",
			compress:   false,
			expectGzip: false,
		},
		{
			name:       "text file with compression",
			fileData:   "Hello, World!",
			mediaType:  "text/plain",
			compress:   true,
			expectGzip: true,
		},
		{
			name:       "json file without compression",
			fileData:   `{"key": "value"}`,
			mediaType:  "application/json",
			compress:   false,
			expectGzip: false,
		},
		{
			name:       "json file with compression",
			fileData:   `{"key": "value"}`,
			mediaType:  "application/json",
			compress:   true,
			expectGzip: true,
		},
		{
			name:       "empty file",
			fileData:   "",
			mediaType:  "text/plain",
			compress:   false,
			expectGzip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			// Create temporary file
			tempDir := t.TempDir()
			filePath := filepath.Join(tempDir, "test.txt")
			err := os.WriteFile(filePath, []byte(tt.fileData), 0644)
			r.NoError(err)

			// Create v1.File spec
			fileSpec := v1.File{
				Type:      runtime.NewUnversionedType("file"),
				Path:      filePath,
				MediaType: tt.mediaType,
				Compress:  tt.compress,
			}

			// Get blob
			b, err := file.GetV1FileBlob(fileSpec, tempDir)
			r.NoError(err)
			r.NotNil(b)

			// Test blob properties
			if sizeAware, ok := b.(blob.SizeAware); ok {
				size := sizeAware.Size()
				r.GreaterOrEqual(size, int64(0))
			}

			if digestAware, ok := b.(blob.DigestAware); ok {
				digest, ok := digestAware.Digest()
				r.True(ok)
				r.NotEmpty(digest)
			}

			// Test reading data
			reader, err := b.ReadCloser()
			r.NoError(err)
			defer func() {
				r.NoError(reader.Close())
			}()

			data, err := io.ReadAll(reader)
			r.NoError(err)

			if tt.expectGzip {
				// Decompress gzipped data
				gzReader, err := gzip.NewReader(bytes.NewReader(data))
				r.NoError(err)
				defer func() {
					r.NoError(gzReader.Close())
				}()

				decompressedData, err := io.ReadAll(gzReader)
				r.NoError(err)
				r.Equal(tt.fileData, string(decompressedData))

				// Test media type for compressed blob
				if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
					mediaType, known := mediaTypeAware.MediaType()
					r.True(known)
					r.Equal(tt.mediaType+"+gzip", mediaType)
				}
			} else {
				r.Equal(tt.fileData, string(data))

				// Test media type for uncompressed blob
				if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
					mediaType, known := mediaTypeAware.MediaType()
					r.True(known)
					r.Equal(tt.mediaType, mediaType)
				}
			}
		})
	}
}

func TestGetV1FileBlob_FileNotFound(t *testing.T) {
	tempDir := t.TempDir()
	r := require.New(t)

	// Create v1.File spec with non-existent file
	fileSpec := v1.File{
		Type: runtime.NewUnversionedType("file"),
		Path: tempDir + "/non/existent/file.txt",
	}

	// Get blob should fail
	b, err := file.GetV1FileBlob(fileSpec, tempDir)
	r.Error(err)
	r.Nil(b)
	r.Contains(err.Error(), "failed to open path")
}

func TestGetV1FileBlob_EmptyPath(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()

	// Create v1.File spec with empty path
	fileSpec := v1.File{
		Type: runtime.NewUnversionedType("file"),
		Path: "",
	}

	// Get blob should fail
	b, err := file.GetV1FileBlob(fileSpec, tempDir)
	r.Error(err)
	r.Nil(b)
}

func TestGetV1FileBlob_BinaryFile(t *testing.T) {
	r := require.New(t)
	// Create binary data
	binaryData := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD, 0xFC}

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "binary.bin")
	err := os.WriteFile(filePath, binaryData, 0644)
	r.NoError(err)

	// Create v1.File spec
	fileSpec := v1.File{
		Type:      runtime.NewUnversionedType("file"),
		Path:      filePath,
		MediaType: "application/octet-stream",
		Compress:  false,
	}

	// Get blob
	b, err := file.GetV1FileBlob(fileSpec, tempDir)
	r.NoError(err)
	r.NotNil(b)

	// Test reading data
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() {
		r.NoError(reader.Close())
	}()

	data, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(binaryData, data)

	// Test media type
	if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
		mediaType, known := mediaTypeAware.MediaType()
		r.True(known)
		r.Equal("application/octet-stream", mediaType)
	}
}

func TestGetV1FileBlob_MultipleReads(t *testing.T) {
	r := require.New(t)

	// Create test file
	testData := "Hello, World! This is a test file for multiple reads."

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test.txt")
	err := os.WriteFile(filePath, []byte(testData), 0644)
	r.NoError(err)

	// Create v1.File spec
	fileSpec := v1.File{
		Type:      runtime.NewUnversionedType("file"),
		Path:      filePath,
		MediaType: "text/plain",
		Compress:  false,
	}

	// Get blob
	b, err := file.GetV1FileBlob(fileSpec, tempDir)
	r.NoError(err)
	r.NotNil(b)

	// Test multiple reads
	for i := 0; i < 3; i++ {
		reader, err := b.ReadCloser()
		r.NoError(err)

		data, err := io.ReadAll(reader)
		r.NoError(err)
		r.Equal(testData, string(data))

		err = reader.Close()
		r.NoError(err)
	}
}

func TestGetV1FileBlob_Compression(t *testing.T) {
	r := require.New(t)

	// Create repetitive data that compresses well
	repetitiveData := bytes.Repeat([]byte("This is repetitive data that should compress well. "), 1000)

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "repetitive.txt")
	err := os.WriteFile(filePath, repetitiveData, 0644)
	r.NoError(err)

	// Test without compression
	fileSpecUncompressed := v1.File{
		Type:      runtime.NewUnversionedType("file"),
		Path:      filePath,
		MediaType: "text/plain",
		Compress:  false,
	}

	blobUncompressed, err := file.GetV1FileBlob(fileSpecUncompressed, tempDir)
	r.NoError(err)

	readerUncompressed, err := blobUncompressed.ReadCloser()
	r.NoError(err)
	defer func() {
		r.NoError(readerUncompressed.Close())
	}()

	dataUncompressed, err := io.ReadAll(readerUncompressed)
	r.NoError(err)
	uncompressedSize := len(dataUncompressed)

	// Test with compression
	fileSpecCompressed := v1.File{
		Type:      runtime.NewUnversionedType("file"),
		Path:      filePath,
		MediaType: "text/plain",
		Compress:  true,
	}

	blobCompressed, err := file.GetV1FileBlob(fileSpecCompressed, tempDir)
	r.NoError(err)

	readerCompressed, err := blobCompressed.ReadCloser()
	r.NoError(err)
	defer func() {
		r.NoError(readerCompressed.Close())
	}()

	dataCompressed, err := io.ReadAll(readerCompressed)
	r.NoError(err)
	compressedSize := len(dataCompressed)

	// Verify compression actually reduced size
	r.Less(compressedSize, uncompressedSize)

	// Verify decompression works correctly
	gzReader, err := gzip.NewReader(bytes.NewReader(dataCompressed))
	r.NoError(err)
	defer func() {
		r.NoError(gzReader.Close())
	}()

	decompressedData, err := io.ReadAll(gzReader)
	r.NoError(err)
	r.Equal(repetitiveData, decompressedData)
}
