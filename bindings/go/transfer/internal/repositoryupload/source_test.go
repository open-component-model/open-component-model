package repositoryupload

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
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
