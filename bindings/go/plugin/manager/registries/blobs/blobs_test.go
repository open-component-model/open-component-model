package blobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

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
