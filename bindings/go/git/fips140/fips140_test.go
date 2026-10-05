// Package fips140 runs the git resource repository with GODEBUG=fips140=only.
//
// Git identifies objects by SHA-1, which strict FIPS 140-3 mode rejects. These
// tests catch code paths that hash with SHA-1 outside the download's
// fips140.WithoutEnforcement scope. The mode is fixed per process, so the
// directive below applies to this package's test binary only.
//go:debug fips140=only
package fips140

import (
	"crypto/fips140"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/repository"
	"ocm.software/open-component-model/bindings/go/git/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestResourceDigestAndDownload(t *testing.T) {
	r := require.New(t)
	r.True(fips140.Enforced(), "test binary must run with fips140=only")

	path, commit := newRepository(t)
	dir := t.TempDir()
	repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &dir})

	res := &descriptor.Resource{Access: &v1.Git{Type: runtime.NewUnversionedType("git"), Repository: path, Ref: "refs/heads/main"}}
	pinned, err := repo.ProcessResourceDigest(t.Context(), res, nil)
	r.NoError(err)
	r.Equal("SHA-256", pinned.Digest.HashAlgorithm)

	var spec v1.Git
	r.NoError(access.Scheme.Convert(pinned.Access, &spec))
	r.Equal(commit.String(), spec.Commit)

	b, err := repo.DownloadResource(t.Context(), pinned, nil)
	r.NoError(err)
	rc, err := b.ReadCloser()
	r.NoError(err)
	r.NoError(rc.Close())
}

// newRepository builds a bare repository with one commit on main. go-git hashes
// the fixture's objects with SHA-1, so the fixture is built without enforcement.
func newRepository(t *testing.T) (path string, commit plumbing.Hash) {
	t.Helper()
	fips140.WithoutEnforcement(func() {
		r := require.New(t)
		work := t.TempDir()
		repo, err := git.PlainInit(work, false)
		r.NoError(err)
		cfg, err := repo.Config()
		r.NoError(err)
		cfg.Commit.GpgSign = config.OptBoolFalse
		r.NoError(repo.SetConfig(cfg))
		r.NoError(repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))

		tree, err := repo.Worktree()
		r.NoError(err)
		r.NoError(os.WriteFile(filepath.Join(work, "README.md"), []byte("fips\n"), 0o600))
		_, err = tree.Add("README.md")
		r.NoError(err)
		commit, err = tree.Commit("fips\n", &git.CommitOptions{Author: &object.Signature{
			Name: "OCM fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0).UTC(),
		}})
		r.NoError(err)

		path = filepath.Join(t.TempDir(), "fixture.git")
		_, err = git.PlainClone(path, &git.CloneOptions{URL: work, Bare: true})
		r.NoError(err)
	})
	return path, commit
}
