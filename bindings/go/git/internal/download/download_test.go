package download

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	for _, head := range []string{"valid HEAD", "missing HEAD"} {
		t.Run(head, func(t *testing.T) {
			r := require.New(t)
			fixture := newRepository(t)
			if head == "missing HEAD" {
				r.NoError(fixture.Git.Storer.RemoveReference("refs/heads/main"))
			}
			r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/feature", fixture.Second)))
			r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/tags/feature", fixture.First)))
			r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/releases/stable", fixture.First)))
			inner, err := fixture.Git.Reference("refs/tags/annotated", true)
			r.NoError(err)
			tag := storeObject(t, fixture.Git, &object.Tag{
				Name:       "nested",
				Tagger:     object.Signature{Name: "OCM fixture", Email: "fixture@example.invalid", When: time.Unix(1700000000, 0).UTC()},
				Message:    "nested release\n",
				TargetType: plumbing.TagObject,
				Target:     inner.Hash(),
			})
			r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/tags/nested", tag)))

			for _, tc := range []struct {
				name, ref, commit string
				want              plumbing.Hash
				missingHeadError  string
			}{
				{"default HEAD", "HEAD", "", fixture.Second, "cannot fetch git repository: transport failed; check repository access and server trust: reference not found"},
				{"short main branch", "main", "", fixture.Second, `cannot resolve git ref: reference name escapes the reference storage: "main" is not under refs/ nor a valid pseudo-ref`},
				{"qualified main branch", "refs/heads/main", "", fixture.Second, "cannot resolve git ref: reference not found"},
				{"branch wins over same-named tag", "feature", "", fixture.Second, ""},
				{"qualified feature branch", "refs/heads/feature", "", fixture.Second, ""},
				{"remote tracking branch", "refs/remotes/origin/feature", "", fixture.Second, ""},
				{"qualified tag wins over same-named branch", "refs/tags/feature", "", fixture.First, ""},
				{"custom ref namespace", "refs/releases/stable", "", fixture.First, ""},
				{"short lightweight tag", "v1", "", fixture.First, ""},
				{"qualified lightweight tag", "refs/tags/v1", "", fixture.First, ""},
				{"short annotated tag", "annotated", "", fixture.First, ""},
				{"qualified annotated tag", "refs/tags/annotated", "", fixture.First, ""},
				{"short nested tag", "nested", "", fixture.First, ""},
				{"qualified nested tag", "refs/tags/nested", "", fixture.First, ""},
				{"pinned commit only", "", fixture.First.String(), fixture.First, ""},
				{"pinned commit overrides HEAD", "HEAD", fixture.First.String(), fixture.First, ""},
				{"pinned commit overrides short main", "main", fixture.First.String(), fixture.First, ""},
				{"pinned commit overrides qualified main", "refs/heads/main", fixture.First.String(), fixture.First, ""},
				{"pinned commit ignores deleted branch", "refs/heads/deleted", fixture.First.String(), fixture.First, ""},
			} {
				t.Run(tc.name, func(t *testing.T) {
					r := require.New(t)
					spec := &v1.Git{Repository: fixture.Path, Ref: tc.ref, Commit: tc.commit}
					result, err := Download(t.Context(), spec, nil, Options{TempDir: t.TempDir()})
					if head == "missing HEAD" && tc.missingHeadError != "" {
						r.ErrorContains(err, tc.missingHeadError)
						r.Nil(result)
						return
					}
					r.NoError(err)
					r.Equal(tc.want.String(), result.Commit)
					r.Equal(tc.commit, spec.Commit)
					r.NotEmpty(readBlob(t, result.Blob))
				})
			}
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

func TestDownloadFailureCleanup(t *testing.T) {
	fixture := newRepository(t)
	for _, tc := range []struct {
		name, ref, commit string
		limit             int64
		cancel            bool
		wantError         string
		wantCause         error
	}{
		{name: "limit", ref: "main", limit: 1, wantError: "git archive exceeds the maximum size"},
		{name: "missing ref", ref: "absent", wantError: `cannot resolve git ref: reference name escapes the reference storage: "absent" is not under refs/ nor a valid pseudo-ref`},
		{name: "missing commit", commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", wantError: "cannot read git revision: object not found", wantCause: plumbing.ErrObjectNotFound},
		{name: "already canceled", ref: "main", cancel: true, wantError: "cannot download git repository: context canceled", wantCause: context.Canceled},
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
			r.ErrorContains(err, tc.wantError)
			if tc.wantCause != nil {
				r.ErrorIs(err, tc.wantCause)
			}
			r.Nil(result)
			files, err := os.ReadDir(dir)
			r.NoError(err)
			r.Empty(files)
		})
	}
}

func TestDownloadInProgressCancellationCleanup(t *testing.T) {
	for _, ref := range []string{"HEAD", "main"} {
		t.Run(ref, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			started := make(chan *http.Request, 1)
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
				started <- req
				select {
				case <-req.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			type outcome struct {
				result *Result
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := Download(ctx, &v1.Git{Repository: server.URL + "/repo.git", Ref: ref}, nil, Options{TempDir: dir})
				done <- outcome{result: result, err: err}
			}()

			select {
			case req := <-started:
				r.Equal("/repo.git/info/refs", req.URL.Path)
				r.Equal("git-upload-pack", req.URL.Query().Get("service"))
			case got := <-done:
				t.Fatalf("download returned before Git discovery: %v", got.err)
			}
			files, err := os.ReadDir(dir)
			r.NoError(err)
			r.Len(files, 1)
			r.True(files[0].IsDir())
			r.True(strings.HasPrefix(files[0].Name(), "ocm-git-repository-"))

			// Cancel only once the download has allocated storage and reached the server.
			cancel()
			got := <-done
			r.ErrorIs(got.err, context.Canceled)
			r.ErrorContains(got.err, "cannot fetch git repository")
			r.Nil(got.result)
			files, err = os.ReadDir(dir)
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
