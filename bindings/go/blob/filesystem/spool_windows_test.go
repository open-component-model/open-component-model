package filesystem_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// Windows keeps the name of the spool file while it is open and deletes the file when the
// blob is removed.
func TestSpool_FileIsVisibleUntilRemoved(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()

	spooled, err := filesystem.Spool(streamedBlob{"streamed content of unknown size"}, dir, "")
	r.NoError(err)
	entries, err := os.ReadDir(dir)
	r.NoError(err)
	r.Len(entries, 1)
	r.True(strings.HasPrefix(entries[0].Name(), "ocm-blob-"))

	r.NoError(spooled.Remove())
	entries, err = os.ReadDir(dir)
	r.NoError(err)
	r.Empty(entries)
}
