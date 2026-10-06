package download

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/stretchr/testify/require"
)

// The tree holds entries a checkout could not reproduce: names that collide on
// case-insensitive file systems, a name invalid on Windows and symlinks pointing
// outside the tree or nowhere.
func TestArchiveUsesGitTree(t *testing.T) {
	r := require.New(t)

	repo, err := git.Init(memory.NewStorage(), nil)
	r.NoError(err)

	store := func(obj objectEncoder) plumbing.Hash { return storeObject(t, repo, obj) }
	blob := func(content string) plumbing.Hash { return storeBlob(t, repo, content) }

	sub := store(&object.Tree{Entries: []object.TreeEntry{
		{Name: "file", Mode: filemode.Deprecated, Hash: blob("nested")},
	}})
	commit := store(&object.Commit{TreeHash: store(&object.Tree{Entries: []object.TreeEntry{
		{Name: "A", Mode: filemode.Regular, Hash: blob("upper")},
		{Name: "a", Mode: filemode.Executable, Hash: blob("lower")},
		{Name: "absolute", Mode: filemode.Symlink, Hash: blob("/etc/passwd")},
		{Name: `back\slash`, Mode: filemode.Regular, Hash: blob("literal backslash")},
		{Name: "colon:name", Mode: filemode.Regular, Hash: blob("colon")},
		{Name: "dangling", Mode: filemode.Symlink, Hash: blob("../does-not-exist")},
		{Name: "dir.c", Mode: filemode.Regular, Hash: blob("before directory in Git order")},
		{Name: "dir", Mode: filemode.Dir, Hash: sub},
		{Name: "vendor", Mode: filemode.Submodule, Hash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
	}})})
	c, err := repo.CommitObject(commit)
	r.NoError(err)

	file, err := os.CreateTemp(t.TempDir(), "archive-*.tar.gz")
	r.NoError(err)

	b, _, err := archive(t.Context(), c, file, Options{})
	r.NoError(err)

	type entry struct {
		typeflag byte
		mode     int64
		content  string
	}
	got := map[string]entry{}
	var names []string
	compressed := readBlob(t, b)
	uncompressed := gunzipArchive(t, compressed)

	tr := tar.NewReader(bytes.NewReader(uncompressed))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)
		r.Zero(h.Uid, h.Name)
		r.Zero(h.Gid, h.Name)
		r.Empty(h.Uname, h.Name)
		r.Empty(h.Gname, h.Name)
		r.Equal(time.Unix(0, 0).UTC(), h.ModTime.UTC(), h.Name)

		data, err := io.ReadAll(tr)
		r.NoError(err)
		if h.Typeflag == tar.TypeSymlink {
			data = []byte(h.Linkname)
		}
		names = append(names, h.Name)
		got[h.Name] = entry{h.Typeflag, h.Mode, string(data)}
	}

	r.Equal([]string{"A", "a", "absolute", `back\slash`, "colon:name", "dangling", "dir", "dir/file", "dir.c", "vendor"}, names)

	r.Equal(map[string]entry{
		"A":          {tar.TypeReg, 0o644, "upper"},
		"a":          {tar.TypeReg, 0o755, "lower"},
		"absolute":   {tar.TypeSymlink, 0o777, "/etc/passwd"},
		`back\slash`: {tar.TypeReg, 0o644, "literal backslash"},
		"colon:name": {tar.TypeReg, 0o644, "colon"},
		"dangling":   {tar.TypeSymlink, 0o777, "../does-not-exist"},
		"dir.c":      {tar.TypeReg, 0o644, "before directory in Git order"},
		"dir":        {tar.TypeDir, 0o755, ""},
		"dir/file":   {tar.TypeReg, 0o644, "nested"},
		"vendor":     {tar.TypeDir, 0o755, ""},
	}, got)
}
