package download

import (
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

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/transport"
	"github.com/go-git/go-git/v6/storage"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
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
			// Refs whose names run below the path of another ref, which git allows across namespaces.
			r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/release", fixture.Second)))
			r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/tags/release/v1", fixture.First)))
			r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/tags/x", fixture.Second)))
			r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/remotes/x/y", fixture.First)))
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
				// wantRef is the full name of the selecting ref; for HEAD without a
				// branch to point to it stays HEAD.
				wantRef          string
				missingHeadError string
				// missingHeadCause is the sentinel the missing-HEAD error must wrap, when
				// resolveRef is the one that reports it.
				missingHeadCause error
			}{
				{name: "default HEAD", ref: "HEAD", want: fixture.Second, wantRef: "refs/heads/main", missingHeadError: "cannot fetch git repository: transport failed; check repository access and server trust: reference not found"},
				{name: "short main branch", ref: "main", want: fixture.Second, wantRef: "refs/heads/main", missingHeadError: `cannot resolve git ref: reference not found: "main"`, missingHeadCause: plumbing.ErrReferenceNotFound},
				{name: "qualified main branch", ref: "refs/heads/main", want: fixture.Second, wantRef: "refs/heads/main", missingHeadError: `cannot resolve git ref: reference not found: "refs/heads/main"`, missingHeadCause: plumbing.ErrReferenceNotFound},
				{name: "branch wins over same-named tag", ref: "feature", want: fixture.Second, wantRef: "refs/heads/feature"},
				{name: "remote tracking branch", ref: "refs/remotes/origin/feature", want: fixture.Second, wantRef: "refs/heads/feature"},
				{name: "qualified tag wins over same-named branch", ref: "refs/tags/feature", want: fixture.First, wantRef: "refs/tags/feature"},
				{name: "custom ref namespace", ref: "refs/releases/stable", want: fixture.First, wantRef: "refs/releases/stable"},
				{name: "short tag below a branch name", ref: "release/v1", want: fixture.First, wantRef: "refs/tags/release/v1"},
				{name: "short remote ref below a tag name", ref: "x/y", want: fixture.First, wantRef: "refs/remotes/x/y"},
				{name: "short lightweight tag", ref: "v1", want: fixture.First, wantRef: "refs/tags/v1"},
				{name: "short annotated tag", ref: "annotated", want: fixture.First, wantRef: "refs/tags/annotated"},
				{name: "short nested tag", ref: "nested", want: fixture.First, wantRef: "refs/tags/nested"},
				{name: "pinned commit only", commit: fixture.First.String(), want: fixture.First},
				{name: "pinned commit overrides HEAD", ref: "HEAD", commit: fixture.First.String(), want: fixture.First, wantRef: "refs/heads/main"},
				{name: "pinned commit with short branch", ref: "feature", commit: fixture.First.String(), want: fixture.First, wantRef: "refs/heads/feature"},
				{name: "pinned commit with short tag", ref: "v1", commit: fixture.First.String(), want: fixture.First, wantRef: "refs/tags/v1"},
				{name: "pinned commit keeps unknown short ref", ref: "gone", commit: fixture.First.String(), want: fixture.First, wantRef: "gone"},
				{name: "pinned commit ignores deleted branch", ref: "refs/heads/deleted", commit: fixture.First.String(), want: fixture.First, wantRef: "refs/heads/deleted"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					r := require.New(t)
					spec := &v1.Git{Repository: fixture.Path, Ref: tc.ref, Commit: tc.commit}
					result, err := download(t.Context(), spec, nil, Options{AllowLocal: true, TempDir: t.TempDir()})
					pinned, pinErr := Pin(t.Context(), spec, nil, Options{AllowLocal: true})
					if head == "missing HEAD" && tc.missingHeadError != "" {
						r.ErrorContains(err, tc.missingHeadError)
						if tc.missingHeadCause != nil {
							r.ErrorIs(err, tc.missingHeadCause)
						}
						r.Nil(result)
						r.Error(pinErr, "a ref the fetch cannot resolve is not pinned")
						return
					}
					r.NoError(err)
					r.Equal(tc.want.String(), result.Commit)
					if head == "missing HEAD" && tc.ref == "HEAD" {
						r.Equal("HEAD", result.Ref, "HEAD without a branch to point to stays HEAD")
					} else {
						r.Equal(tc.wantRef, result.Ref)
					}
					r.Equal(tc.commit, spec.Commit)
					r.NotEmpty(result.Data)
					r.NoError(pinErr)
					r.Equal(tc.want.String(), pinned.Commit, "Pin selects the commit the fetch selects")
				})
			}
		})
	}
}

func TestDownloadURLFragment(t *testing.T) {
	fixture := newRepository(t)
	r := require.New(t)
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/feature", fixture.First)))

	for _, tc := range []struct {
		name, fragment, ref string
		want                plumbing.Hash
	}{
		{name: "branch", fragment: "branch=feature", want: fixture.First},
		{name: "tag", fragment: "tag=v1", want: fixture.First},
		{name: "commit", fragment: "commit=" + fixture.First.String(), want: fixture.First},
		{name: "commit overrides ref", fragment: "commit=" + fixture.First.String(), ref: "main", want: fixture.First},
		{name: "same branch as ref", fragment: "branch=main", ref: "main", want: fixture.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			spec := &v1.Git{Repository: fixture.Path + "#" + tc.fragment, Ref: tc.ref}
			result, err := download(t.Context(), spec, nil, Options{AllowLocal: true, TempDir: t.TempDir()})
			r.NoError(err)
			r.Equal(tc.want.String(), result.Commit)
		})
	}
}

func TestDownloadURLFragmentNotSent(t *testing.T) {
	r := require.New(t)

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests = append(requests, req.URL.RequestURI())
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	_, err := download(t.Context(), &v1.Git{Repository: server.URL + "/repo.git#branch=main"}, nil, Options{TempDir: t.TempDir(), HTTPClient: server.Client()})
	r.ErrorContains(err, "repository not found")
	r.Equal([]string{"/repo.git/info/refs?service=git-upload-pack"}, requests)
}

func TestDownloadArchive(t *testing.T) {
	r := require.New(t)
	fixture := newRepository(t)
	dir := t.TempDir()
	streamed, err := download(t.Context(), &v1.Git{Repository: fixture.Path, Ref: "main"}, nil, Options{AllowLocal: true, TempDir: dir})
	r.NoError(err)

	b := NewBlob(t.Context(), &v1.Git{Repository: fixture.Path, Ref: "main"}, nil, Options{AllowLocal: true, TempDir: dir})
	mt, ok := b.MediaType()
	r.True(ok)
	r.Equal("application/x-tgz", mt)
	data := readBlob(t, b)
	r.Equal(streamed.Data, data, "the blob streams what Stream writes")
	r.Equal(digest.FromBytes(data), streamed.Digest)

	// The first read pins the commit, so a moved ref does not change later reads.
	r.NoError(fixture.Git.Storer.SetReference(plumbing.NewHashReference("refs/heads/main", fixture.First)))
	r.Equal(data, readBlob(t, b))

	// Nothing of the archive is stored.
	files, err := os.ReadDir(dir)
	r.NoError(err)
	r.Empty(files)
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
	r.NotEqual(digest.FromBytes(uncompressed), streamed.Digest)

	// Use the same stdlib default compressor as v1, without custom gzip metadata.
	var recompressed bytes.Buffer
	writer := gzip.NewWriter(&recompressed)
	_, err = writer.Write(uncompressed)
	r.NoError(err)
	r.NoError(writer.Close())
	r.Equal(data, recompressed.Bytes())
}

func TestBlobFromClone(t *testing.T) {
	t.Run("first read serves the clone and removes it", func(t *testing.T) {
		r := require.New(t)
		fixture := newRepository(t)
		dir := t.TempDir()
		access := &v1.Git{Repository: fixture.Path, Ref: "main"}
		want, err := download(t.Context(), access, nil, Options{AllowLocal: true, TempDir: dir})
		r.NoError(err)

		clone, err := Fetch(t.Context(), access, nil, Options{AllowLocal: true, TempDir: dir})
		r.NoError(err)
		b := NewBlobFromClone(t.Context(), clone, access, nil, Options{AllowLocal: true, TempDir: dir})
		// Without the source, only the clone can serve the read.
		r.NoError(os.RemoveAll(fixture.Path))
		r.Equal(want.Data, readBlob(t, b))
		files, err := os.ReadDir(dir)
		r.NoError(err)
		r.Empty(files, "closing the first reader removes the clone")

		rc, err := b.ReadCloser()
		r.NoError(err)
		_, err = io.ReadAll(rc)
		r.ErrorContains(errors.Join(err, rc.Close()), "cannot fetch git repository", "later reads fetch again")
	})

	t.Run("an unread clone is removed when the context ends", func(t *testing.T) {
		r := require.New(t)
		fixture := newRepository(t)
		dir := t.TempDir()
		ctx, cancel := context.WithCancel(t.Context())
		clone, err := Fetch(ctx, &v1.Git{Repository: fixture.Path, Ref: "main"}, nil, Options{AllowLocal: true, TempDir: dir})
		r.NoError(err)
		NewBlobFromClone(ctx, clone, &v1.Git{Repository: fixture.Path, Ref: "main"}, nil, Options{AllowLocal: true, TempDir: dir})
		files, err := os.ReadDir(dir)
		r.NoError(err)
		r.Len(files, 1)

		cancel()
		r.Eventually(func() bool {
			files, err := os.ReadDir(dir)
			return err == nil && len(files) == 0
		}, 5*time.Second, 10*time.Millisecond)
	})
}

// downloaded is a Result with the archive Stream wrote.
type downloaded struct {
	*Result
	Data []byte
}

// download streams the archive of access into memory.
func download(ctx context.Context, access *v1.Git, creds runtime.Typed, opts Options) (*downloaded, error) {
	var buf bytes.Buffer
	result, err := Stream(ctx, access, creds, &buf, opts)
	if err != nil {
		return nil, err
	}
	return &downloaded{Result: result, Data: buf.Bytes()}, nil
}

func readBlob(t *testing.T, b blob.ReadOnlyBlob) []byte {
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
		cancel            bool
		wantError         string
		wantCause         error
	}{
		{name: "missing ref", ref: "absent", wantError: "cannot resolve git ref", wantCause: plumbing.ErrReferenceNotFound},
		{name: "missing commit", commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", wantError: "cannot fetch git repository"},
		{name: "already canceled", ref: "main", cancel: true, wantError: "cannot fetch git repository: context canceled", wantCause: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			dir := t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}

			result, err := download(ctx, &v1.Git{Repository: fixture.Path, Ref: tc.ref, Commit: tc.commit}, nil, Options{AllowLocal: true, TempDir: dir})
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

// errAtRef makes a repository's reference storage fail with a sentinel error for one
// exact reference name while behaving normally for every other name.
type errAtRef struct {
	storage.Storer
	name plumbing.ReferenceName
	err  error
}

func (s errAtRef) Reference(name plumbing.ReferenceName) (*plumbing.Reference, error) {
	if name == s.name {
		return nil, s.err
	}
	return s.Storer.Reference(name)
}

func TestResolveRefUnreadableCandidate(t *testing.T) {
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	readErr := errors.New("disk on fire")
	for _, tc := range []struct {
		name, ref  string
		unreadable plumbing.ReferenceName
		existing   []plumbing.ReferenceName
		wantErr    error
		wantRef    plumbing.ReferenceName
	}{
		// A fetched branch is looked up before any tag, so an unreadable tag does not matter.
		{name: "unreadable tag behind a fetched branch", ref: "main", unreadable: "refs/tags/main", existing: []plumbing.ReferenceName{"refs/heads/main", "refs/remotes/origin/main"}, wantRef: "refs/heads/main"},
		// A fetched branch outranks a tag; if it cannot be read, the tag must not win.
		{name: "unreadable fetched branch before existing tag", ref: "main", unreadable: "refs/remotes/origin/main", existing: []plumbing.ReferenceName{"refs/heads/main", "refs/tags/main"}, wantErr: readErr},
		{name: "unreadable candidate after not-found ones", ref: "main", unreadable: "refs/heads/main", wantErr: readErr},
		{name: "unreadable candidate after the hit", ref: "main", unreadable: "refs/remotes/main", existing: []plumbing.ReferenceName{"refs/heads/main"}, wantRef: "refs/heads/main"},
		{name: "all candidates absent", ref: "main", wantErr: plumbing.ErrReferenceNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			base := memory.NewStorage()
			for _, n := range tc.existing {
				r.NoError(base.SetReference(plumbing.NewHashReference(n, hash)))
			}
			repo, err := git.Init(errAtRef{Storer: base, name: tc.unreadable, err: readErr})
			r.NoError(err)

			_, got, err := resolveRef(repo, tc.ref)
			if tc.wantErr != nil {
				r.ErrorIs(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantRef, got)
		})
	}
}

func TestResolveRefBelowRefFile(t *testing.T) {
	r := require.New(t)
	hash := plumbing.NewHash("1111111111111111111111111111111111111111")
	repo, err := git.PlainInit(t.TempDir(), true)
	r.NoError(err)
	for _, n := range []plumbing.ReferenceName{"refs/heads/release", "refs/remotes/origin/release", "refs/tags/release/v1"} {
		r.NoError(repo.Storer.SetReference(plumbing.NewHashReference(n, hash)))
	}

	_, got, err := resolveRef(repo, "release/v1")
	r.NoError(err)
	r.Equal(plumbing.ReferenceName("refs/tags/release/v1"), got)

	_, _, err = resolveRef(repo, "release/missing")
	r.ErrorIs(err, plumbing.ErrReferenceNotFound)
	r.ErrorContains(err, `"release/missing"`)
}

// On disk, a name that leaves the reference storage is invalid under every rule.
func TestResolveRefInvalidName(t *testing.T) {
	r := require.New(t)
	repo, err := git.PlainInit(t.TempDir(), true)
	r.NoError(err)
	for _, name := range []string{"../x", "x/"} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			_, _, err := resolveRef(repo, name)
			r.ErrorIs(err, plumbing.ErrInvalidReferenceName)
			r.NotErrorIs(err, plumbing.ErrReferenceNotFound)
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
				result *downloaded
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := download(ctx, &v1.Git{Repository: server.URL + "/repo.git", Ref: ref}, nil, Options{TempDir: dir})
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
			r.True(strings.HasPrefix(files[0].Name(), "ocm-git-"))

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

// TestRejectsLocalRepository checks that every transport rejects a local repository
// unless AllowLocal is set, also for callers that skip Git.Validate.
func TestRejectsLocalRepository(t *testing.T) {
	fixture := newRepository(t)
	for _, tc := range []struct{ name, repository string }{
		{name: "plain path", repository: fixture.Path},
		{name: "file URL", repository: "file://" + fixture.Path},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			spec := &v1.Git{Repository: tc.repository, Ref: "main"}

			_, err := download(t.Context(), spec, nil, Options{TempDir: t.TempDir()})
			r.ErrorIs(err, endpoint.ErrLocalRepository)
			_, err = Fetch(t.Context(), spec, nil, Options{TempDir: t.TempDir()})
			r.ErrorIs(err, endpoint.ErrLocalRepository)
			_, err = Pin(t.Context(), spec, nil, Options{})
			r.ErrorIs(err, endpoint.ErrLocalRepository)

			ep, err := endpoint.Parse(tc.repository)
			r.NoError(err)
			_, _, err = ClientOptions(t.Context(), ep, nil, Options{})
			r.ErrorIs(err, endpoint.ErrLocalRepository)

			result, err := download(t.Context(), spec, nil, Options{TempDir: t.TempDir(), AllowLocal: true})
			r.NoError(err)
			r.Equal(fixture.Second.String(), result.Commit)
		})
	}
}

func TestArchiveSizeBoundary(t *testing.T) {
	r := require.New(t)

	fixture := newRepository(t)
	spec := &v1.Git{Repository: fixture.Path, Commit: fixture.First.String()}
	result, err := download(t.Context(), spec, nil, Options{AllowLocal: true, TempDir: t.TempDir()})
	r.NoError(err)

	data := result.Data
	size := int64(len(data))
	r.Greater(int64(len(gunzipArchive(t, data))), size, "the limit applies to compressed bytes only")

	result, err = download(t.Context(), spec, nil, Options{AllowLocal: true, TempDir: t.TempDir(), MaxArchiveSize: size})
	r.NoError(err)
	r.Equal(data, result.Data)

	dir := t.TempDir()
	result, err = download(t.Context(), spec, nil, Options{AllowLocal: true, TempDir: dir, MaxArchiveSize: size - 1})
	r.ErrorContains(err, "git archive exceeds the maximum size")
	r.Nil(result)
	entries, err := os.ReadDir(dir)
	r.NoError(err)
	r.Empty(entries)
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
		err := TransportError(t.Context(), "fetch", tc.err)
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

func TestRedactURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://user:token@example.invalid/org/repo.git", "https://xxxxx@example.invalid/org/repo.git"},
		{"https://token@example.invalid/org/repo.git", "https://xxxxx@example.invalid/org/repo.git"},
		{"ssh://git@example.invalid/org/repo.git", "ssh://xxxxx@example.invalid/org/repo.git"},
		{"https://user:p@ss@example.invalid/org/repo.git", "https://xxxxx@example.invalid/org/repo.git"},
		{"https://example.invalid/org/a@b.git", "https://example.invalid/org/a@b.git"},
		{"https://example.invalid?ref=a@b", "https://example.invalid?ref=a@b"},
		{"https://example.invalid/org/repo.git", "https://example.invalid/org/repo.git"},
		{"/srv/git/repo.git", "/srv/git/repo.git"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			r := require.New(t)
			r.Equal(tc.want, RedactURL(tc.in))
		})
	}
}
