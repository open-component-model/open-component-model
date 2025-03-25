package oci

import (
	"context"
	"fmt"

	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
)

func NewURLPathResolver(baseURL string) *URLPathResolver {
	return &URLPathResolver{
		BaseURL: baseURL,
	}
}

// URLPathResolver is a Resolver that resolves references to URLs for Component Versions and Resources.
// It uses a BaseURL and a BaseClient to get a remote store for a reference.

type URLPathResolver struct {
	BaseURL    string
	BaseClient remote.Client
}

func (resolver *URLPathResolver) SetClient(client remote.Client) {
	resolver.BaseClient = client
}

func (resolver *URLPathResolver) BaseReference(component string) string {
	return fmt.Sprintf("%s/component-descriptors/%s", resolver.BaseURL, component)
}

func (resolver *URLPathResolver) ComponentVersionReference(component, version string) string {
	return fmt.Sprintf("%s:%s", resolver.BaseReference(component), version)
}

func (resolver *URLPathResolver) TargetResourceReference(srcReference string) (targetReference string, err error) {
	ref, err := registry.ParseReference(srcReference)
	if err != nil {
		return "", err
	}
	ref.Registry = resolver.BaseURL
	return ref.String(), nil
}

func (resolver *URLPathResolver) StoreForReference(_ context.Context, reference string) (Store, error) {
	repo, err := remote.NewRepository(reference)
	if err != nil {
		return nil, err
	}
	if resolver.BaseClient != nil {
		repo.Client = resolver.BaseClient
	}
	return repo, nil
}

var _ Resolver = (*URLPathResolver)(nil)
