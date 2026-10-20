package repositoryupload

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/uploadtest"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
)

// sourceStubRepository returns a fixed blob from DownloadResource and derives no
// credential identity.
type sourceStubRepository struct {
	repository.ResourceRepository
	blob      blob.ReadOnlyBlob
	downloads int
}

func (s *sourceStubRepository) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (s *sourceStubRepository) DownloadResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	s.downloads++
	return s.blob, nil
}

// TestSourceRemoteUsesDownloadResource: a remote source is read with
// DownloadResource exactly once, and the media type the blob reports is used.
func TestSourceRemoteUsesDownloadResource(t *testing.T) {
	r := require.New(t)
	src := inmemory.New(strings.NewReader("hello"), inmemory.WithMediaType("text/plain"))
	repo := &sourceStubRepository{blob: src}
	u := &Uploader{ResourceRepository: repo}
	spec := &uploadv1alpha1.RepositoryUploadSpec{
		ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "c", Version: "v"},
	}

	b, mediaType, err := u.source(t.Context(), spec, &descriptor.Resource{})
	r.NoError(err)
	r.Same(src, b, "the blob that DownloadResource returns is used as is")
	r.Equal("text/plain", mediaType)
	r.Equal(1, repo.downloads)
}

// pinnableRepo serves Content through the version tag and the content of view once pinned.
type pinnableRepo struct {
	uploadtest.StubComponentVersionRepository
	view   *uploadtest.StubComponentVersionRepository
	pinned string
}

func (p *pinnableRepo) PinComponentVersion(_, _, digest string) (repository.ComponentVersionRepository, error) {
	p.pinned = digest
	return p.view, nil
}

func (p *pinnableRepo) ResolveComponentVersionDigest(context.Context, string, string) (string, error) {
	panic("not used")
}

func pinnedLocalBlobSpec(digest string) *uploadv1alpha1.RepositoryUploadSpec {
	return &uploadv1alpha1.RepositoryUploadSpec{
		ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{
			Repository: &runtime.Raw{Type: runtime.NewVersionedType("OCIRepository", "v1"), Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"ghcr.io/source"}`)},
			Component:  "ocm.software/test",
			Version:    "1.0.0",
			Digest:     digest,
		},
	}
}

func TestSource_PinnedLocalBlob(t *testing.T) {
	src := &descriptor.Resource{ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "res", Version: "1.0.0"}}}
	const pin = "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

	t.Run("reads from the pinned artifact, not through the tag", func(t *testing.T) {
		r := require.New(t)
		repo := &pinnableRepo{
			StubComponentVersionRepository: uploadtest.StubComponentVersionRepository{Content: []byte("moved tag")},
			view:                           &uploadtest.StubComponentVersionRepository{Content: []byte("pinned")},
		}
		u := &Uploader{RepoProvider: &uploadtest.StubRepositoryProvider{Repository: repo}}

		b, _, err := u.source(t.Context(), pinnedLocalBlobSpec(pin), src)
		r.NoError(err)
		r.Equal(pin, repo.pinned)
		rc, err := b.ReadCloser()
		r.NoError(err)
		t.Cleanup(func() { _ = rc.Close() })
		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.Equal("pinned", string(got))
	})

	t.Run("fails on a repository that cannot be pinned", func(t *testing.T) {
		r := require.New(t)
		u := &Uploader{RepoProvider: &uploadtest.StubRepositoryProvider{Repository: &uploadtest.StubComponentVersionRepository{Content: []byte("moved tag")}}}

		_, _, err := u.source(t.Context(), pinnedLocalBlobSpec(pin), src)
		r.ErrorContains(err, "cannot read component version ocm.software/test:1.0.0 pinned to "+pin)
	})
}
