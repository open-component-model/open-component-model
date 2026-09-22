package download

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
)

func TestDownloadRevisions(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/feature", fixture.First)))
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/tags/feature", fixture.Second)))
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/releases/stable", fixture.First)))
	for _, tc := range []struct {
		ref, commit string
		want        plumbing.Hash
	}{
		{"HEAD", "", fixture.Second},
		{"main", "", fixture.Second},
		{"refs/heads/main", "", fixture.Second},
		{"feature", "", fixture.First},
		{"refs/heads/feature", "", fixture.First},
		{"refs/remotes/origin/feature", "", fixture.First},
		{"refs/tags/feature", "", fixture.Second},
		{"refs/releases/stable", "", fixture.First},
		{"v1", "", fixture.First},
		{"annotated", "", fixture.First},
		{"refs/tags/annotated", "", fixture.First},
		{"", fixture.First.String(), fixture.First},
		{"HEAD", fixture.First.String(), fixture.First},
		{"main", fixture.First.String(), fixture.First},
		{"refs/heads/deleted", fixture.First.String(), fixture.First},
	} {
		t.Run(tc.ref+tc.commit, func(t *testing.T) {
			r := require.New(t)

			dir := t.TempDir()
			spec := &v1.Git{Repository: fixture.Path, Ref: tc.ref, Commit: tc.commit}
			result, err := Download(t.Context(), spec, nil, Options{TempDir: dir})
			r.NoError(err)
			r.Equal(tc.want.String(), result.Commit)
			r.Equal(tc.commit, spec.Commit)
		})
	}
}

func TestDownloadArchive(t *testing.T) {
	r := require.New(t)
	fixture := newRepository(t)
	dir := t.TempDir()
	result, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Ref: "main"}, nil, Options{TempDir: dir})
	r.NoError(err)

	b := result.Blob
	mt, ok := b.MediaType()
	r.True(ok)
	r.Equal("application/x-tgz", mt)
	data := readBlob(t, b)
	r.Equal(data, readBlob(t, b))
	raw, ok := b.Digest()
	r.True(ok)
	r.Equal(digest.FromBytes(data).String(), raw)
	r.Equal(raw, result.Digest.String())

	// The archive file outlives the download and belongs to the caller.
	files, err := os.ReadDir(dir)
	r.NoError(err)
	r.Len(files, 1)
	r.True(strings.HasSuffix(files[0].Name(), ".tar.gz"))
	gz, err := gzip.NewReader(bytes.NewReader(data))
	r.NoError(err)
	r.True(gz.ModTime.IsZero())
	r.Empty(gz.Name)
	r.Empty(gz.Comment)
	r.Empty(gz.Extra)
	r.Equal(uint8(255), gz.OS)
	uncompressed, err := io.ReadAll(gz)
	r.NoError(err)
	r.NoError(gz.Close())
	r.NotEqual(digest.FromBytes(uncompressed), result.Digest)

	// Use the same stdlib default compressor as v1, without custom gzip metadata.
	var recompressed bytes.Buffer
	writer := gzip.NewWriter(&recompressed)
	_, err = writer.Write(uncompressed)
	r.NoError(err)
	r.NoError(writer.Close())
	r.Equal(data, recompressed.Bytes())

	tr := tar.NewReader(bytes.NewReader(uncompressed))
	var names []string
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
		names = append(names, h.Name)
	}
	r.Equal([]string{"README.md", "docs", "docs/guide.txt", "link", "run.sh"}, names)
}

func readBlob(t *testing.T, b *filesystem.Blob) []byte {
	t.Helper()

	r := require.New(t)

	rc, err := b.ReadCloser()
	r.NoError(err)

	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.NoError(rc.Close())

	return data
}

func gunzipArchive(t *testing.T, data []byte) []byte {
	t.Helper()
	r := require.New(t)

	gz, err := gzip.NewReader(bytes.NewReader(data))
	r.NoError(err)
	uncompressed, err := io.ReadAll(gz)
	r.NoError(err)
	r.NoError(gz.Close())
	return uncompressed
}

func TestDownloadDeterministic(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	var previous []byte
	for range 3 {
		result, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Commit: fixture.First.String()}, nil, Options{TempDir: t.TempDir()})
		r.NoError(err)

		data := readBlob(t, result.Blob)
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

			result, err := Download(ctx, &v1.Git{Repository: fixture.Path, Ref: tc.ref, Commit: tc.commit}, nil, Options{TempDir: dir, MaxArchiveSize: tc.limit})
			r.Error(err)
			r.Nil(result)
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
	hash := storeObject(t, fixture.Git, tree)

	commit, err := fixture.Git.CommitObject(fixture.First)
	r.NoError(err)

	commit.TreeHash = hash
	commitHash := storeObject(t, fixture.Git, commit)
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/submodule", commitHash)))

	result, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Commit: commitHash.String()}, nil, Options{TempDir: t.TempDir()})
	r.NoError(err)

	tr := tar.NewReader(bytes.NewReader(gunzipArchive(t, readBlob(t, result.Blob))))
	h, err := tr.Next()
	r.NoError(err)
	r.Equal("vendor", h.Name)
	r.Equal(byte(tar.TypeDir), h.Typeflag)
	r.Equal(int64(0o755), h.Mode)
	r.Zero(h.Size)
	_, err = tr.Next()
	r.ErrorIs(err, io.EOF, "submodule is an empty placeholder even when its target commit is present")
}

func TestDownloadRefWithoutRemoteHEAD(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	r.NoError(fixture.Git.Storer.RemoveReference("refs/heads/main"))
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/feature", fixture.Second)))
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/tags/feature", fixture.First)))
	for _, tc := range []struct {
		ref  string
		want plumbing.Hash
	}{
		{"feature", fixture.Second},
		{"refs/heads/feature", fixture.Second},
		{"refs/tags/feature", fixture.First},
		{"v1", fixture.First},
		{"refs/tags/v1", fixture.First},
		{"annotated", fixture.First},
		{"refs/tags/annotated", fixture.First},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			r := require.New(t)

			result, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Ref: tc.ref}, nil, Options{TempDir: t.TempDir()})
			r.NoError(err)
			r.Equal(tc.want.String(), result.Commit)
			r.NotEmpty(readBlob(t, result.Blob))
		})
	}
}

func TestDownloadNestedAnnotatedTag(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	inner, err := fixture.Git.Reference("refs/tags/annotated", true)
	r.NoError(err)
	tag := &object.Tag{
		Name:       "nested",
		Tagger:     object.Signature{Name: "OCM fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0).UTC()},
		Message:    "nested release\n",
		TargetType: plumbing.TagObject,
		Target:     inner.Hash(),
	}
	hash := storeObject(t, fixture.Git, tag)
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/tags/nested", hash)))

	for _, danglingHEAD := range []bool{false, true} {
		t.Run(fmt.Sprintf("danglingHEAD=%t", danglingHEAD), func(t *testing.T) {
			r := require.New(t)
			if danglingHEAD {
				r.NoError(fixture.Git.Storer.RemoveReference("refs/heads/main"))
			}
			for _, ref := range []string{"nested", "refs/tags/nested"} {
				t.Run(ref, func(t *testing.T) {
					r := require.New(t)

					result, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Ref: ref}, nil, Options{TempDir: t.TempDir()})
					r.NoError(err)
					r.Equal(fixture.First.String(), result.Commit)
					r.NotEmpty(readBlob(t, result.Blob))
				})
			}
		})
	}
}

func TestPinnedCommitWithoutRemoteHEAD(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	r.NoError(fixture.Git.Storer.RemoveReference("refs/heads/main"))

	result, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Commit: fixture.First.String(), Ref: "refs/heads/main"}, nil, Options{TempDir: t.TempDir()})
	r.NoError(err)
	r.Equal(fixture.First.String(), result.Commit)
	r.NotNil(result.Blob)
}

func TestArchiveSizeBoundary(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	spec := &v1.Git{Repository: fixture.Path, Commit: fixture.First.String()}
	result, err := Download(t.Context(), spec, nil, Options{TempDir: t.TempDir()})
	r.NoError(err)

	data := readBlob(t, result.Blob)
	size := result.Blob.Size()
	r.Equal(int64(len(data)), size)
	r.Greater(int64(len(gunzipArchive(t, data))), size, "the limit applies to compressed bytes only")

	result, err = Download(t.Context(), spec, nil, Options{TempDir: t.TempDir(), MaxArchiveSize: size})
	r.NoError(err)
	r.NotNil(result.Blob)
	r.Equal(data, readBlob(t, result.Blob))

	dir := t.TempDir()
	result, err = Download(t.Context(), spec, nil, Options{TempDir: dir, MaxArchiveSize: size - 1})
	r.ErrorContains(err, "git archive exceeds the maximum size")
	r.Nil(result)
	entries, err := os.ReadDir(dir)
	r.NoError(err)
	r.Empty(entries)
}

func TestPinnedArchiveContainsSelectedCommit(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	first, err := fixture.Git.CommitObject(fixture.First)
	r.NoError(err)

	file, err := os.CreateTemp(t.TempDir(), "archive-*.tar.gz")
	r.NoError(err)

	expected, expectedDigest, err := archive(t.Context(), first, file, Options{})
	r.NoError(err)

	actual, err := Download(t.Context(), &v1.Git{Repository: fixture.Path, Commit: fixture.First.String(), Ref: "main"}, nil, Options{TempDir: t.TempDir()})
	r.NoError(err)
	r.Equal(readBlob(t, expected), readBlob(t, actual.Blob))
	r.Equal(expectedDigest, actual.Digest)
}

func TestTransportErrorMessages(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{git.ErrRepositoryNotExists, "fetch: repository not found: repository does not exist"},
		{
			// What a server says about a rejected login is the actionable part.
			fmt.Errorf("%w: remote: Invalid username or password", transport.ErrAuthenticationRequired),
			"fetch: authentication required: authentication required: remote: Invalid username or password",
		},
		{transport.ErrAuthorizationFailed, "fetch: authorization failed: authorization failed"},
		{
			errors.New(`remote: https://user:token@example.invalid/repo.git rejected`),
			"fetch: transport failed; check repository access and server trust: remote: https://xxxxx@example.invalid/repo.git rejected",
		},
	} {
		err := transportError(t.Context(), "fetch", tc.err)
		require.EqualError(t, err, tc.want)
		require.NotContains(t, err.Error(), "token")
	}
}

type objectEncoder interface {
	Encode(plumbing.EncodedObject) error
}

func storeObject(t *testing.T, repo *git.Repository, obj objectEncoder) plumbing.Hash {
	t.Helper()
	r := require.New(t)
	encoded := repo.Storer.NewEncodedObject()
	r.NoError(obj.Encode(encoded))
	hash, err := repo.Storer.SetEncodedObject(encoded)
	r.NoError(err)
	return hash
}

func storeBlob(t *testing.T, repo *git.Repository, content string) plumbing.Hash {
	t.Helper()
	r := require.New(t)
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

	store := func(obj objectEncoder) plumbing.Hash { return storeObject(t, repo, obj) }
	blob := func(content string) plumbing.Hash { return storeBlob(t, repo, content) }

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
