package filesystem_test

import (
	"archive/tar"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func TestGetBlobFromPath_DefaultTarBytes(t *testing.T) {
	for _, reproducible := range []bool{false, true} {
		name := "default"
		if reproducible {
			name = "reproducible"
		}
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			r.NoError(os.Mkdir(filepath.Join(dir, "sub"), 0o755))
			r.NoError(os.WriteFile(filepath.Join(dir, "sub", "file"), []byte("content"), 0o644))
			var expected bytes.Buffer
			tw := tar.NewWriter(&expected)
			for _, name := range []string{".", "sub", "sub/file"} {
				info, err := os.Stat(filepath.Join(dir, name))
				r.NoError(err)
				h, err := tar.FileInfoHeader(info, "")
				r.NoError(err)
				h.Name = name
				if info.IsDir() {
					h.Name += "/"
				}
				if reproducible {
					h.ModTime, h.AccessTime, h.ChangeTime = time.Unix(0, 0), time.Unix(0, 0), time.Unix(0, 0)
					h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "", ""
					h.Mode &= 0o777
				}
				r.NoError(tw.WriteHeader(h))
				if !info.IsDir() {
					_, err = tw.Write([]byte("content"))
					r.NoError(err)
				}
			}
			r.NoError(tw.Close())
			b, err := filesystem.GetBlobFromPath(t.Context(), dir, filesystem.DirOptions{Reproducible: reproducible})
			r.NoError(err)
			actual, err := readAllFromBlob(b)
			r.NoError(err)
			r.Equal(expected.Bytes(), actual, "default bytes retain ./ and sub/ entries")
		})
	}
}

func TestTarWriterLayoutOptions(t *testing.T) {
	for _, tt := range []struct {
		name  string
		opt   filesystem.DirOptions
		names []string
	}{
		{name: "defaults", names: []string{"./", "sub/"}},
		{name: "omit root", opt: filesystem.DirOptions{OmitRoot: true}, names: []string{"sub/"}},
		{name: "omit slash", opt: filesystem.DirOptions{OmitDirTrailingSlash: true}, names: []string{".", "sub"}},
		{name: "git layout", opt: filesystem.DirOptions{OmitRoot: true, OmitDirTrailingSlash: true}, names: []string{"sub"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			source := fstest.MapFS{"sub": &fstest.MapFile{Mode: fs.ModeDir | 0o755}}
			var output bytes.Buffer
			writer := filesystem.NewTarWriter(&output, tt.opt)
			for _, name := range []string{".", "sub"} {
				info, err := fs.Stat(source, name)
				r.NoError(err)
				r.NoError(writer.WriteEntry(t.Context(), name, info, source))
			}
			r.NoError(writer.Close())
			reader := tar.NewReader(&output)
			var names []string
			for range tt.names {
				h, err := reader.Next()
				r.NoError(err)
				names = append(names, h.Name)
			}
			r.Equal(tt.names, names)
			_, err := reader.Next()
			r.ErrorIs(err, io.EOF)
		})
	}
}
