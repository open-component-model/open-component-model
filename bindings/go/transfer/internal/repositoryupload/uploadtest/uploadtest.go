// Package uploadtest provides test doubles for the repository uploader tests.
package uploadtest

import (
	"bytes"
	"context"
	"errors"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	_ repository.ResourceRepository                 = (*StubResourceRepository)(nil)
	_ repository.ComponentVersionRepository         = (*StubComponentVersionRepository)(nil)
	_ repository.ComponentVersionRepositoryProvider = (*StubRepositoryProvider)(nil)
	_ credentials.Resolver                          = StubCredentials(nil)
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

// StubComponentVersionRepository serves Content as every local resource and records the last
// request in Component, Version and Identity. Its other methods are not implemented and panic.
type StubComponentVersionRepository struct {
	repository.ComponentVersionRepository
	Content            []byte
	Component, Version string
	Identity           runtime.Identity
}

func (s *StubComponentVersionRepository) GetLocalResource(_ context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	s.Component, s.Version, s.Identity = component, version, identity
	return inmemory.New(bytes.NewReader(s.Content), inmemory.WithSize(int64(len(s.Content)))), nil, nil
}

// StubRepositoryProvider returns Repository for every repository specification and derives no
// credential consumer identity. Its other methods are not implemented and panic.
type StubRepositoryProvider struct {
	repository.ComponentVersionRepositoryProvider
	Repository repository.ComponentVersionRepository
}

func (p *StubRepositoryProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(context.Context, runtime.Typed) (runtime.Identity, error) {
	return nil, errors.New("no credential consumer identity")
}

func (p *StubRepositoryProvider) GetComponentVersionRepository(context.Context, runtime.Typed, runtime.Typed) (repository.ComponentVersionRepository, error) {
	return p.Repository, nil
}
