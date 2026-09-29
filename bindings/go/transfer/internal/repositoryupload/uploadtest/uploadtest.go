// Package uploadtest holds test doubles shared by the repository uploader tests.
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

// ResourceRepo serves Content from DownloadResource and derives no source credential identity.
type ResourceRepo struct {
	repository.ResourceRepository
	Content   []byte
	MediaType string
}

func (s *ResourceRepo) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (s *ResourceRepo) DownloadResource(_ context.Context, _ *descriptor.Resource, _ runtime.Typed) (blob.ReadOnlyBlob, error) {
	return inmemory.New(bytes.NewReader(s.Content), inmemory.WithSize(int64(len(s.Content))), inmemory.WithMediaType(s.MediaType)), nil
}

// CredentialsByType resolves credentials by the type attribute of the consumer identity.
type CredentialsByType map[string]runtime.Typed

func (c CredentialsByType) Resolve(_ context.Context, id runtime.Identity) (runtime.Typed, error) {
	typ, ok := id["type"]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	cred, ok := c[typ]
	if !ok {
		return nil, credentials.ErrNotFound
	}
	return cred, nil
}
