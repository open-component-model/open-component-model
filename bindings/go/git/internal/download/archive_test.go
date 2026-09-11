package download

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestArchivePreservesSymlinksAndMetadata(t *testing.T) {
	r := require.New(t)

	root := t.TempDir()
	outside := t.TempDir()
	r.NoError(os.WriteFile(filepath.Join(outside, "secret"), []byte("outside content"), 0o600))
	r.NoError(os.Mkdir(filepath.Join(root, "dir"), 0o700))
	r.NoError(os.WriteFile(filepath.Join(root, "dir", "file"), []byte("inside content"), 0o600))
	r.NoError(os.Mkdir(filepath.Join(root, ".git"), 0o700))
	r.NoError(os.WriteFile(filepath.Join(root, ".git", "config"), []byte("git metadata"), 0o600))

	links := map[string]string{
		"relative":           "dir/file",
		"absolute":           filepath.Join(outside, "secret"),
		"dangling":           "does-not-exist",
		"directory":          "dir",
		"external-directory": outside,
		"parent":             "../outside",
		"unchanged-target":   "dir/../dir/file",
	}

	for name, target := range links {
		r.NoError(os.Symlink(target, filepath.Join(root, name)))
	}

	b, err := archive(t.Context(), root, Options{TempDir: t.TempDir()})
	r.NoError(err)

	defer b.Close()
	tr := tar.NewReader(bytes.NewReader(readBlob(t, b)))
	names := []string{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		names = append(names, h.Name)
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(h.Name)))
		r.NoError(err)

		expected, err := tar.FileInfoHeader(info, "")
		r.NoError(err)
		r.Equal(expected.Mode, h.Mode, h.Name)
		r.Zero(h.Uid, h.Name)
		r.Zero(h.Gid, h.Name)
		r.Empty(h.Uname, h.Name)
		r.Empty(h.Gname, h.Name)
		r.Equal(time.Unix(0, 0).UTC(), h.ModTime.UTC(), h.Name)

		data, err := io.ReadAll(tr)
		r.NoError(err)

		if target, ok := links[h.Name]; ok {
			r.Equal(byte(tar.TypeSymlink), h.Typeflag, h.Name)
			r.Equal(target, h.Linkname, h.Name)
			r.Empty(data, h.Name)
		} else if h.Name == "dir/file" {
			r.Equal("inside content", string(data))
		}
	}
	r.Equal([]string{"./", "absolute", "dangling", "dir/", "dir/file", "directory", "external-directory", "parent", "relative", "unchanged-target"}, names)
}

func TestArchiveIgnoresModificationTime(t *testing.T) {
	r := require.New(t)

	root := t.TempDir()
	file := filepath.Join(root, "file")
	r.NoError(os.WriteFile(file, []byte("content"), 0o644))

	before, err := archive(t.Context(), root, Options{TempDir: t.TempDir()})
	r.NoError(err)

	defer before.Close()
	later := time.Unix(1900000000, 0)
	r.NoError(os.Chtimes(file, later, later))

	after, err := archive(t.Context(), root, Options{TempDir: t.TempDir()})
	r.NoError(err)

	defer after.Close()
	r.Equal(readBlob(t, before), readBlob(t, after))
}
