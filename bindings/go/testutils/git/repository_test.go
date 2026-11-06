package git_test

import (
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	testgit "ocm.software/open-component-model/bindings/go/testutils/git"
)

func commitCount(t *testing.T, path string) int {
	t.Helper()
	r := require.New(t)
	repo, err := gogit.PlainOpen(path)
	r.NoError(err)
	iter, err := repo.Log(&gogit.LogOptions{All: true})
	r.NoError(err)
	count := 0
	r.NoError(iter.ForEach(func(*object.Commit) error { count++; return nil }))
	return count
}

func TestRepositoryDefaults(t *testing.T) {
	r := require.New(t)
	path, first := testgit.Repository(t, testgit.Options{})
	r.False(first.IsZero())
	r.FileExists(filepath.Join(path, "README.md"))
	r.Equal(2, commitCount(t, path))
}

func TestRepositoryNestedFilePath(t *testing.T) {
	r := require.New(t)
	path, _ := testgit.Repository(t, testgit.Options{Commits: []testgit.Commit{
		{File: "nested/dir/config.yaml", Content: "key: value\n"},
	}})
	r.FileExists(filepath.Join(path, "nested", "dir", "config.yaml"))
}

func TestRepositoryIdenticalCommits(t *testing.T) {
	r := require.New(t)
	path, _ := testgit.Repository(t, testgit.Options{Commits: []testgit.Commit{
		{Content: "same\n"},
		{Content: "same\n"},
	}})
	r.Equal(2, commitCount(t, path))
}

func TestRepositoryBareWithoutCommits(t *testing.T) {
	r := require.New(t)
	path, first := testgit.Repository(t, testgit.Options{Commits: []testgit.Commit{}, Bare: true})
	r.True(first.IsZero())
	r.DirExists(path)
}
