package filesystem_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// Windows keeps the name of the spool file while it is open and deletes the file with its
// last handle, also when a reader outlives the blob.
func TestSpool_FileIsDeletedWithItsLastHandle(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()
	const content = "streamed content of unknown size"

	spooled, err := filesystem.Spool(streamedBlob{content}, dir, "")
	r.NoError(err)
	entries, err := os.ReadDir(dir)
	r.NoError(err)
	r.Len(entries, 1)
	r.True(strings.HasPrefix(entries[0].Name(), "ocm-blob-"))

	reader, err := spooled.ReadCloser()
	r.NoError(err)
	r.NoError(spooled.Remove())

	data, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(content, string(data))
	r.NoError(reader.Close())

	entries, err = os.ReadDir(dir)
	r.NoError(err)
	r.Empty(entries)
}
