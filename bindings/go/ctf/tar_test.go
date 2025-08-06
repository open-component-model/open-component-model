package ctf_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
)

func Test_Archive(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)

	archive, err := ctf.OpenCTF(ctx, ctf.OpenCTFOptions{
		Path:    t.TempDir(),
		Format:  ctf.FormatDirectory,
		Flag:    ctf.O_RDWR,
		TempDir: t.TempDir(),
	})
	r.NoError(err)

	testBlob := inmemory.New(bytes.NewReader([]byte("test")))

	r.NoError(archive.SaveBlob(ctx, testBlob))

	t.Run("Directory", func(t *testing.T) {
		newArchive := t.TempDir()
		r.NoError(ctf.ArchiveDirectory(ctx, archive, newArchive))
	})
	t.Run("TAR", func(t *testing.T) {
		newArchive := filepath.Join(t.TempDir(), "archive.tar")
		r.NoError(ctf.Archive(ctx, archive, newArchive, ctf.FormatTAR))
	})
}
