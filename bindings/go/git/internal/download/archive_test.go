package download

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/stretchr/testify/require"
)

// The tree holds entries a checkout could not reproduce: names that collide on
// case-insensitive file systems, a name invalid on Windows and symlinks pointing
// outside the tree or nowhere.
func TestArchiveUsesGitTree(t *testing.T) {
	r := require.New(t)

	repo, err := git.Init(memory.NewStorage(), nil)
	r.NoError(err)

	store := func(obj interface {
		Encode(plumbing.EncodedObject) error
	},
	) plumbing.Hash {
		encoded := repo.Storer.NewEncodedObject()
		r.NoError(obj.Encode(encoded))
		hash, err := repo.Storer.SetEncodedObject(encoded)
		r.NoError(err)
		return hash
	}
	blob := func(content string) plumbing.Hash {
		encoded := repo.Storer.NewEncodedObject()
		encoded.SetType(plumbing.BlobObject)
		w, err := encoded.Writer()
		r.NoError(err)
		_, err = io.WriteString(w, content)
		r.NoError(err)
		r.NoError(w.Close())
		hash, err := repo.Storer.SetEncodedObject(encoded)
		r.NoError(err)
		return hash
	}

	sub := store(&object.Tree{Entries: []object.TreeEntry{
		{Name: "file", Mode: filemode.Deprecated, Hash: blob("nested")},
	}})
	commit := store(&object.Commit{TreeHash: store(&object.Tree{Entries: []object.TreeEntry{
		{Name: "A", Mode: filemode.Regular, Hash: blob("upper")},
		{Name: "a", Mode: filemode.Executable, Hash: blob("lower")},
		{Name: "absolute", Mode: filemode.Symlink, Hash: blob("/etc/passwd")},
		{Name: "colon:name", Mode: filemode.Regular, Hash: blob("colon")},
		{Name: "dangling", Mode: filemode.Symlink, Hash: blob("../does-not-exist")},
		{Name: "dir", Mode: filemode.Dir, Hash: sub},
		{Name: "vendor", Mode: filemode.Submodule, Hash: plumbing.NewHash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
	}})})
	c, err := repo.CommitObject(commit)
	r.NoError(err)

	file, err := os.CreateTemp(t.TempDir(), "archive-*.tar")
	r.NoError(err)

	b, err := archive(t.Context(), c, file, Options{})
	r.NoError(err)

	type entry struct {
		typeflag byte
		mode     int64
		content  string
	}
	got := map[string]entry{}
	var names []string
	tr := tar.NewReader(bytes.NewReader(readBlob(t, b)))
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

	r.Equal([]string{"A", "a", "absolute", "colon:name", "dangling", "dir/file"}, names)
	r.Equal(map[string]entry{
		"A":          {tar.TypeReg, 0o644, "upper"},
		"a":          {tar.TypeReg, 0o755, "lower"},
		"absolute":   {tar.TypeSymlink, 0o777, "/etc/passwd"},
		"colon:name": {tar.TypeReg, 0o644, "colon"},
		"dangling":   {tar.TypeSymlink, 0o777, "../does-not-exist"},
		"dir/file":   {tar.TypeReg, 0o644, "nested"},
	}, got)
}
