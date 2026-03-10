package examples

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// TestExample_InMemoryBlob demonstrates how to create an in-memory blob from a
// string and read its contents back.
func TestExample_InMemoryBlob(t *testing.T) {
	r := require.New(t)

	// Create an in-memory blob from a string reader.
	b := inmemory.New(strings.NewReader("hello, OCM!"))

	// Read the full contents.
	rc, err := b.ReadCloser()
	r.NoError(err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal("hello, OCM!", string(data))
}

// TestExample_InMemoryBlobWithMetadata shows how to attach size, digest, and
// media-type metadata to an in-memory blob at creation time.
func TestExample_InMemoryBlobWithMetadata(t *testing.T) {
	r := require.New(t)

	content := "structured payload"
	expectedDigest, err := digest.FromReader(strings.NewReader(content))
	r.NoError(err)

	b := inmemory.New(
		strings.NewReader(content),
		inmemory.WithSize(int64(len(content))),
		inmemory.WithDigest(expectedDigest.String()),
		inmemory.WithMediaType("application/json"),
	)

	// Verify all metadata is present.
	r.True(b.HasPrecalculatedSize())
	r.Equal(int64(len(content)), b.Size())

	r.True(b.HasPrecalculatedDigest())
	dig, known := b.Digest()
	r.True(known)
	r.Equal(expectedDigest.String(), dig)

	mediaType, known := b.MediaType()
	r.True(known)
	r.Equal("application/json", mediaType)
}

// TestExample_FilesystemBlob demonstrates creating a blob from a file on disk
// using the filesystem package's GetBlobFromPath helper.
func TestExample_FilesystemBlob(t *testing.T) {
	r := require.New(t)

	// Write a temporary file.
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	r.NoError(os.WriteFile(path, []byte("file content"), 0o644))

	// Create a read-only blob from the file path.
	b, err := filesystem.GetBlobFromPath(t.Context(), path, filesystem.DirOptions{})
	r.NoError(err)

	rc, err := b.ReadCloser()
	r.NoError(err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal("file content", string(data))
}

// TestExample_CopyBlob shows how to copy blob data to a writer using blob.Copy,
// which automatically verifies the digest when the source blob is digest-aware.
func TestExample_CopyBlob(t *testing.T) {
	r := require.New(t)

	content := []byte("copy me safely")
	b := inmemory.New(bytes.NewReader(content))

	var buf bytes.Buffer
	r.NoError(blob.Copy(&buf, b))
	r.Equal(content, buf.Bytes())

	// blob.Copy supports repeated reads on buffered blobs.
	var buf2 bytes.Buffer
	r.NoError(blob.Copy(&buf2, b))
	r.Equal(content, buf2.Bytes())
}
