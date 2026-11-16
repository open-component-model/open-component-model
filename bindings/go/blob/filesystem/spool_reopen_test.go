//go:build linux || darwin || windows

package filesystem_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// Where the operating system can open the spool file again, every reader is a file of its
// own instead of a view on the one handle the blob was spooled through.
func TestSpool_ReaderIsAFile(t *testing.T) {
	r := require.New(t)
	spooled, err := filesystem.Spool(streamedBlob{"content"}, t.TempDir(), "")
	r.NoError(err)

	reader, err := spooled.ReadCloser()
	r.NoError(err)
	r.IsType(&os.File{}, reader)
	r.NoError(reader.Close())
	r.NoError(spooled.Remove())
}
