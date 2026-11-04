package ctf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A blob published concurrently after the existence check is kept: publishing does not
// replace it, and the staged file is left for the caller to remove.
func TestPublishBlobFileKeepsExistingBlob(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "sha256.blob")
	r.NoError(os.WriteFile(target, []byte("content"), 0o444))
	before, err := os.Stat(target)
	r.NoError(err)
	staged := filepath.Join(dir, ".upload.1")
	r.NoError(os.WriteFile(staged, []byte("content"), 0o600))

	published, err := publishBlobFile(staged, "sha256.blob")
	r.NoError(err)
	r.False(published, "the staged file stays for the caller to remove")
	after, err := os.Stat(target)
	r.NoError(err)
	r.True(os.SameFile(before, after), "the stored blob is kept, not replaced")
	_, err = os.Stat(staged)
	r.NoError(err)
}
