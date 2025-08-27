package blobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func TestCreateBlobDataLocalFileSuccess(t *testing.T) {
	r := require.New(t)

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	testData := []byte("test data")

	err := os.WriteFile(testFile, testData, 0644)
	r.NoError(err)

	location := types.Location{
		LocationType: types.LocationTypeLocalFile,
		Value:        testFile,
	}

	blob, err := CreateBlobData(location)
	r.NoError(err)
	r.NotNil(blob)

	reader, err := blob.ReadCloser()
	r.NoError(err)
	defer reader.Close()

	data := make([]byte, len(testData))
	n, err := reader.Read(data)
	r.NoError(err)
	r.Equal(len(testData), n)
	r.Equal(testData, data)
}

func TestCreateBlobDataLocalFileFileNotFound(t *testing.T) {
	r := require.New(t)

	location := types.Location{
		LocationType: types.LocationTypeLocalFile,
		Value:        "/nonexistent/path/file.txt",
	}

	blob, err := CreateBlobData(location)
	r.Error(err)
	r.Nil(blob)
	r.Contains(err.Error(), "path does not exist")
}

func TestCreateBlobDataUnsupportedLocationType(t *testing.T) {
	r := require.New(t)

	location := types.Location{
		LocationType: types.LocationTypeRemoteURL,
		Value:        "https://example.com/file.txt",
	}

	blob, err := CreateBlobData(location)
	r.Error(err)
	r.Nil(blob)
	r.Equal("unsupported location type: remoteURL", err.Error())
}

func TestCreateBlobDataUnsupportedLocationTypeUnixNamedPipe(t *testing.T) {
	r := require.New(t)

	location := types.Location{
		LocationType: types.LocationTypeUnixNamedPipe,
		Value:        "/tmp/test.pipe",
	}

	blob, err := CreateBlobData(location)
	r.Error(err)
	r.Nil(blob)
	r.Equal("unsupported location type: unixNamedPipe", err.Error())
}

func TestCreateBlobData_WithMediaType(t *testing.T) {
	// Create a temporary file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("test content")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	// Test with MediaType set
	location := types.Location{
		LocationType: types.LocationTypeLocalFile,
		Value:        tmpFile,
		MediaType:    "text/plain",
	}

	b, err := CreateBlobData(location)
	require.NoError(t, err)
	require.NotNil(t, b)

	// Check if the blob is MediaTypeAware and has the correct media type
	mtAware, ok := b.(blob.MediaTypeAware)
	require.True(t, ok, "blob should implement MediaTypeAware")

	mediaType, known := mtAware.MediaType()
	assert.True(t, known, "media type should be known")
	assert.Equal(t, "text/plain", mediaType, "media type should match")
}

func TestCreateBlobData_WithoutMediaType(t *testing.T) {
	// Create a temporary file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("test content")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	// Test without MediaType set
	location := types.Location{
		LocationType: types.LocationTypeLocalFile,
		Value:        tmpFile,
		// MediaType is empty
	}

	b, err := CreateBlobData(location)
	require.NoError(t, err)
	require.NotNil(t, b)

	// Check if the blob is MediaTypeAware but has no media type set
	mtAware, ok := b.(blob.MediaTypeAware)
	require.True(t, ok, "blob should implement MediaTypeAware")

	_, known := mtAware.MediaType()
	assert.False(t, known, "media type should not be known when not set")
}
