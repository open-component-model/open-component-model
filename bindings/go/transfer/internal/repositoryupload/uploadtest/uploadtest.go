// Package uploadtest provides test doubles for the repository uploader tests.
package uploadtest

import (
	"bytes"
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	_ repository.ResourceRepository = (*StubResourceRepository)(nil)
	_ credentials.Resolver          = StubCredentials(nil)
)

// StubResourceRepository downloads every resource as Content with MediaType and derives no source
// credential identity. Its other methods are not implemented and panic.
type StubResourceRepository struct {
	repository.ResourceRepository
	Content   []byte
	MediaType string
}

func (s *StubResourceRepository) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (s *StubResourceRepository) DownloadResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	return inmemory.New(bytes.NewReader(s.Content), inmemory.WithSize(int64(len(s.Content))), inmemory.WithMediaType(s.MediaType)), nil
}

// StubCredentials resolves the credentials of a consumer identity by its type attribute.
type StubCredentials map[string]runtime.Typed

func (c StubCredentials) Resolve(_ context.Context, id runtime.Identity) (runtime.Typed, error) {
	if cred, ok := c[id["type"]]; ok {
		return cred, nil
	}
	return nil, credentials.ErrNotFound
}
