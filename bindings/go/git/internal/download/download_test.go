package download

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
)

func TestDownloadRevisions(t *testing.T) {
	fixture := newRepository(t)
	require.NoError(t, fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/feature", fixture.First)))
	require.NoError(t, fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/tags/feature", fixture.Second)))
	for _, tc := range []struct {
		ref, commit string
		want        plumbing.Hash
	}{
		{"HEAD", "", fixture.Second},
		{"main", "", fixture.Second},
		{"refs/heads/main", "", fixture.Second},
		{"feature", "", fixture.First},
		{"refs/heads/feature", "", fixture.First},
		{"v1", "", fixture.First},
		{"refs/tags/annotated", "", fixture.First},
		{"", fixture.First.String(), fixture.First},
		{"refs/heads/deleted", fixture.First.String(), fixture.First},
	} {
		t.Run(tc.ref+tc.commit, func(t *testing.T) {
			r := require.New(t)

			dir := t.TempDir()
			spec := &v1.Git{Repository: fixture.Path, Ref: tc.ref, Commit: tc.commit}
			b, commit, err := Download(t.Context(), spec, nil, Options{TempDir: dir})
			r.NoError(err)
			r.Equal(tc.want.String(), commit)
			r.Equal(tc.commit, spec.Commit)

			mt, ok := b.MediaType()
			r.True(ok)
			r.Equal("application/x-tar", mt)

			data := readBlob(t, b)
			r.Equal(data, readBlob(t, b))

			raw, ok := b.Digest()
			r.True(ok)
			r.Equal(digest.FromBytes(data).String(), raw)

			files, err := os.ReadDir(dir)
			r.NoError(err)
			r.Len(files, 1)
			tr := tar.NewReader(bytes.NewReader(data))
			names := []string{}
			for {
				h, err := tr.Next()
				if err == io.EOF {
					break
				}
				r.NoError(err)

				names = append(names, h.Name)
				switch h.Name {
				case "run.sh":
					r.Equal(int64(0o755), h.Mode)
				case "link":
					r.Equal(byte(tar.TypeSymlink), h.Typeflag)
					r.Equal("docs/guide.txt", h.Linkname)
				}
			}
			r.Equal([]string{"README.md", "docs", "docs/guide.txt", "link", "run.sh"}, names)
			r.NoError(b.Close())
			r.NoError(b.Close())

			files, err = os.ReadDir(dir)
			r.NoError(err)
			r.Empty(files)
		})
	}
}

func readBlob(t *testing.T, b *Blob) []byte {
	t.Helper()

	r := require.New(t)

	rc, err := b.ReadCloser()
	r.NoError(err)

	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())

	return data
}

func TestDownloadDeterministic(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	var previous []byte
	for range 3 {
		b, _, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Commit: fixture.First.String()}, nil, Options{TempDir: t.TempDir()})
		r.NoError(err)

		data := readBlob(t, b)
		r.NoError(b.Close())
		if previous != nil {
			r.Equal(previous, data)
		}

		previous = data
	}
}

func TestDownloadFailureCleanup(t *testing.T) {
	fixture := newRepository(t)
	for _, tc := range []struct {
		name, ref, commit string
		limit             int64
		cancel            bool
	}{
		{name: "limit", ref: "main", limit: 1},
		{name: "missing ref", ref: "absent"},
		{name: "missing commit", commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{name: "cancel", ref: "main", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			dir := t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}

			b, _, err := Download(ctx, &v1.Git{Repository: fixture.Path, Ref: tc.ref, Commit: tc.commit}, nil, Options{TempDir: dir, MaxDownloadSize: tc.limit})
			r.Error(err)
			r.Nil(b)
			files, err := os.ReadDir(dir)
			r.NoError(err)
			r.Empty(files)
		})
	}
}

func TestSubmoduleArchive(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	tree := &object.Tree{Entries: []object.TreeEntry{{Name: "vendor", Mode: filemode.Submodule, Hash: fixture.First}}}
	encoded := fixture.Git.Storer.NewEncodedObject()
	r.NoError(tree.Encode(encoded))

	hash, err := fixture.Git.Storer.SetEncodedObject(encoded)
	r.NoError(err)

	commit, err := fixture.Git.CommitObject(fixture.First)
	r.NoError(err)

	commit.TreeHash = hash
	encoded = fixture.Git.Storer.NewEncodedObject()
	r.NoError(commit.Encode(encoded))

	commitHash, err := fixture.Git.Storer.SetEncodedObject(encoded)
	r.NoError(err)
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/submodule", commitHash)))

	b, _, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Commit: commitHash.String()}, nil, Options{TempDir: t.TempDir()})
	r.NoError(err)

	defer b.Close()
	tr := tar.NewReader(bytes.NewReader(readBlob(t, b)))
	h, err := tr.Next()
	r.NoError(err)
	r.Equal("vendor", h.Name)
	r.Equal(byte(tar.TypeDir), h.Typeflag)

	_, err = tr.Next()
	r.ErrorIs(err, io.EOF)
}

func TestPinnedCommitWithoutRemoteHEAD(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	r.NoError(fixture.Git.Storer.RemoveReference("refs/heads/main"))

	b, commit, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Commit: fixture.First.String(), Ref: "refs/heads/main"}, nil, Options{TempDir: t.TempDir()})
	r.NoError(err)
	r.Equal(fixture.First.String(), commit)
	r.NoError(b.Close())
}

func TestArchiveSizeBoundary(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	spec := &v1.Git{Repository: fixture.Path, Commit: fixture.First.String()}
	b, _, err := Download(t.Context(), spec, nil, Options{TempDir: t.TempDir()})
	r.NoError(err)

	size := b.Size()
	r.NoError(b.Close())

	b, _, err = Download(t.Context(), spec, nil, Options{TempDir: t.TempDir(), MaxDownloadSize: size})
	r.NoError(err)
	r.NoError(b.Close())

	dir := t.TempDir()
	b, _, err = Download(t.Context(), spec, nil, Options{TempDir: dir, MaxDownloadSize: size - 1})
	r.Error(err)
	r.Nil(b)
	entries, err := os.ReadDir(dir)
	r.NoError(err)
	r.Empty(entries)
}

func TestPinnedArchiveContainsSelectedCommit(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	root := t.TempDir()
	repo, err := git.PlainCloneContext(t.Context(), root, false, &git.CloneOptions{URL: fixture.Path, NoCheckout: true})
	r.NoError(err)

	worktree, err := repo.Worktree()
	r.NoError(err)
	r.NoError(worktree.Checkout(&git.CheckoutOptions{Hash: fixture.First}))

	expected, err := archive(t.Context(), root, Options{TempDir: t.TempDir()})
	r.NoError(err)

	defer expected.Close()
	actual, _, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Commit: fixture.First.String(), Ref: "main"}, nil, Options{TempDir: t.TempDir()})
	r.NoError(err)

	defer actual.Close()
	r.Equal(readBlob(t, expected), readBlob(t, actual))
}

func TestTransportErrorMessages(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{git.ErrRepositoryNotExists, "fetch: repository not found"},
		{fmt.Errorf("dial: %w", transport.ErrAuthenticationRequired), "fetch: authentication required"},
		{transport.ErrAuthorizationFailed, "fetch: authorization failed"},
		{errors.New("https://user:token@example.invalid rejected"), "fetch: transport failed; check repository access and server trust"},
	} {
		require.EqualError(t, transportError(t.Context(), "fetch", tc.err), tc.want)
	}
}
