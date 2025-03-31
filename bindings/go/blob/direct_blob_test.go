package blob_test

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "ocm.software/open-component-model/bindings/go/blob"
)

func Test_ReadCloserReturnsReader(t *testing.T) {
	r := require.New(t)
	reader := strings.NewReader("test data")
	blob := NewDirectReadOnlyBlob(reader)
	readCloser, err := blob.ReadCloser()
	r.NoError(err)
	r.NotNil(readCloser)
}

func Test_ReadCloserReadsDataCorrectly(t *testing.T) {
	r := require.New(t)
	expectedData := "test data"
	reader := strings.NewReader(expectedData)
	blob := NewDirectReadOnlyBlob(reader)
	readCloser, err := blob.ReadCloser()
	r.NoError(err)
	data, err := io.ReadAll(readCloser)
	r.NoError(err)
	r.Equal(expectedData, string(data))
}

func Test_NewDirectReadOnlyBlobWrapsReader(t *testing.T) {
	r := require.New(t)
	reader := strings.NewReader("test data")
	blob := NewDirectReadOnlyBlob(reader)
	r.NotNil(blob.EagerBufferedReader)
}

func Test_ReadCloserHandlesEmptyReader(t *testing.T) {
	r := require.New(t)
	reader := strings.NewReader("")
	blob := NewDirectReadOnlyBlob(reader)
	readCloser, err := blob.ReadCloser()
	r.NoError(err)
	data, err := io.ReadAll(readCloser)
	r.NoError(err)
	r.Equal("", string(data))
}
