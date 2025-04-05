package blob_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

func TestCopy_DirectBlob(t *testing.T) {
	r := require.New(t)
	blobData := []byte("hello world!")
	directBlob := blob.NewDirectReadOnlyBlob(bytes.NewReader(blobData))
	var buf bytes.Buffer

	err := blob.Copy(&buf, directBlob)
	r.NoError(err, "unexpected error while copying blob")
	r.Equal(blobData, buf.Bytes(), "blob content mismatch")
}

func TestCopy_DirectBlob_ReadError(t *testing.T) {
	r := require.New(t)
	errorReader := &errorReader{}
	directBlob := blob.NewDirectReadOnlyBlob(errorReader)
	var buf bytes.Buffer

	err := blob.Copy(&buf, directBlob)
	r.Error(err, "expected error, got nil")
	r.Contains(err.Error(), "mock read error", "unexpected error message")
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("mock read error")
}
