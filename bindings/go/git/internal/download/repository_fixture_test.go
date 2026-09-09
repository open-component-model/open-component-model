package download

import (
	"io"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
)

type repositoryFixture struct {
	Path          string
	Git           *git.Repository
	First, Second plumbing.Hash
}

func newRepository(t *testing.T) repositoryFixture {
	t.Helper()

	r := require.New(t)

	dir := t.TempDir()
	repo, err := git.PlainInit(dir, true)
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
		encoded.SetSize(int64(len(content)))

		w, err := encoded.Writer()
		r.NoError(err)

		_, err = io.WriteString(w, content)
		r.NoError(err)
		r.NoError(w.Close())

		hash, err := repo.Storer.SetEncodedObject(encoded)
		r.NoError(err)

		return hash
	}

	signature := object.Signature{
		Name:  "OCM fixture",
		Email: "fixture@example.invalid",
		When:  time.Unix(1700000000, 0).UTC(),
	}
	doc := store(&object.Tree{
		Entries: []object.TreeEntry{
			{Name: "guide.txt", Mode: filemode.Regular, Hash: blob("guide\n")},
		},
	})
	entries := []object.TreeEntry{
		{Name: "README.md", Mode: filemode.Regular, Hash: blob("first\n")},
		{Name: "docs", Mode: filemode.Dir, Hash: doc},
		{Name: "link", Mode: filemode.Symlink, Hash: blob("docs/guide.txt")},
		{Name: "run.sh", Mode: filemode.Executable, Hash: blob("#!/bin/sh\necho fixture\n")},
	}

	first := store(&object.Commit{
		Author:    signature,
		Committer: signature,
		Message:   "first\n",
		TreeHash:  store(&object.Tree{Entries: entries}),
	})

	entries[0].Hash = blob("second\n")
	second := store(&object.Commit{
		Author:       signature,
		Committer:    signature,
		Message:      "second\n",
		TreeHash:     store(&object.Tree{Entries: entries}),
		ParentHashes: []plumbing.Hash{first},
	})

	r.NoError(repo.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", second)))
	r.NoError(repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))
	r.NoError(repo.Storer.SetReference(plumbing.NewHashReference("refs/tags/v1", first)))

	tag := store(&object.Tag{
		Name:       "annotated",
		Tagger:     signature,
		Message:    "release\n",
		TargetType: plumbing.CommitObject,
		Target:     first,
	})
	r.NoError(repo.Storer.SetReference(plumbing.NewHashReference("refs/tags/annotated", tag)))

	return repositoryFixture{
		Path:   dir,
		Git:    repo,
		First:  first,
		Second: second,
	}
}
