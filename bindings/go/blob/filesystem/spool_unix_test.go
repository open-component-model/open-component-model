//go:build unix

package filesystem_test

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// On unix the spool file has no name while it is in use, and opening a reader must not give
// it one: a name is what a killed process would leave behind.
func TestSpool_FileHasNoNameWhileInUse(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()
	const content = "streamed content of unknown size"

	spooled, err := filesystem.Spool(streamedBlob{content}, dir, "")
	r.NoError(err)
	entries, err := os.ReadDir(dir)
	r.NoError(err)
	r.Empty(entries)

	rc, err := spooled.ReadCloser()
	r.NoError(err)
	entries, err = os.ReadDir(dir)
	r.NoError(err)
	r.Empty(entries)

	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, string(data))
	r.NoError(rc.Close())
	r.NoError(spooled.Remove())
}
