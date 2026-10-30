package resource

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func Test_ExtractFSFromBlob_CopiesContents(t *testing.T) {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	content := []byte("chart content")
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "chart.tgz", Mode: 0o644, Size: int64(len(content))}))
	_, err := tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	tarPath := filepath.Join(t.TempDir(), "archive.tar")
	require.NoError(t, os.WriteFile(tarPath, archive.Bytes(), 0o600))

	tests := []struct {
		name string
		blob func(t *testing.T) blob.ReadOnlyBlob
	}{
		{
			name: "in-memory direct blob",
			blob: func(t *testing.T) blob.ReadOnlyBlob {
				t.Helper()
				return direct.NewFromBytes(archive.Bytes(), direct.WithMediaType("application/tar"))
			},
		},
		{
			// file readers support io.ReaderAt, so tarfs reads entries lazily from the open file
			name: "file-backed blob",
			blob: func(t *testing.T) blob.ReadOnlyBlob {
				t.Helper()
				b, err := filesystem.GetBlobFromOSPath(tarPath)
				require.NoError(t, err)
				b.SetMediaType("application/tar")
				return b
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			fsys, closeFS, err := extractFSFromBlob(tc.blob(t))
			r.NoError(err)

			out := t.TempDir()
			r.NoError(os.CopyFS(out, fsys))
			r.NoError(closeFS())

			got, err := os.ReadFile(filepath.Join(out, "chart.tgz"))
			r.NoError(err)
			r.Equal(content, got)
		})
	}
}
