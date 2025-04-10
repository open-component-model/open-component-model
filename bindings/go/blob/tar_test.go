package blob_test

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

type mockReadOnlyBlob struct {
	data        []byte
	errorOnRead bool
}

func (m *mockReadOnlyBlob) ReadCloser() (io.ReadCloser, error) {
	if m.errorOnRead {
		return nil, fmt.Errorf("mock read error")
	}
	return io.NopCloser(bytes.NewReader(m.data)), nil
}

func TestArchiveBlob(t *testing.T) {
	r := require.New(t)
	name := "test-blob"
	size := int64(12)
	digest := "test-digest"
	blobData := []byte("hello world!")
	mockBlob := &mockReadOnlyBlob{data: blobData}

	var buf bytes.Buffer
	tarWriter := tar.NewWriter(&buf)

	err := blob.ArchiveBlob(name, size, digest, mockBlob, tarWriter, nil)
	r.NoError(err, "unexpected error while archiving blob")
	t.Cleanup(func() {
		r.NoError(tarWriter.Close())
	})

	// Read back the tar archive to verify contents
	tr := tar.NewReader(&buf)
	header, err := tr.Next()
	r.NoError(err, "error reading tar header")
	r.Equal(name, header.Name, "unexpected tar entry name")
	r.Equal(size, header.Size, "unexpected tar entry size")

	content := make([]byte, size)
	n, err := tr.Read(content)
	r.True(err == nil || err == io.EOF, "unexpected error reading tar content")
	r.Equal(blobData, content[:n], "tar content mismatch")
}

func TestArchiveBlob_ReadError(t *testing.T) {
	r := require.New(t)
	mockBlob := &mockReadOnlyBlob{errorOnRead: true}
	var buf bytes.Buffer
	tarWriter := tar.NewWriter(&buf)

	err := blob.ArchiveBlob("test-blob", 10, "test-digest", mockBlob, tarWriter, nil)
	r.Error(err, "expected error, got nil")
	r.Contains(err.Error(), "mock read error", "unexpected error message")
}
