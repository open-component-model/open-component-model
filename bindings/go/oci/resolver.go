package oci

import (
	"context"
	"fmt"
	"sync"

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
	PlainHTTP  bool

	DisableCache bool

	cacheMu sync.RWMutex
	cache   map[string]Store
}

func (resolver *URLPathResolver) SetClient(client remote.Client) {
	resolver.BaseClient = client
}

func (resolver *URLPathResolver) ComponentVersionReference(component, version string) string {
	return fmt.Sprintf("%s/component-descriptors/%s:%s", resolver.BaseURL, component, version)
}

func (resolver *URLPathResolver) StoreForReference(_ context.Context, reference string) (Store, error) {
	if store, ok := resolver.getFromCache(reference); ok {
		return store, nil
	}

	var store Store
	repo, err := remote.NewRepository(reference)
	if err != nil {
		return nil, err
	}
	if resolver.PlainHTTP {
		repo.PlainHTTP = true
	}
	if resolver.BaseClient != nil {
		repo.Client = resolver.BaseClient
	}
	store = repo

	resolver.addToCache(reference, store)

	return store, nil
}

func (resolver *URLPathResolver) addToCache(reference string, store Store) {
	resolver.cacheMu.Lock()
	defer resolver.cacheMu.Unlock()
	if resolver.cache == nil {
		resolver.cache = make(map[string]Store)
	}
	resolver.cache[reference] = store
}

func (resolver *URLPathResolver) getFromCache(reference string) (Store, bool) {
	resolver.cacheMu.RLock()
	defer resolver.cacheMu.RUnlock()
	store, ok := resolver.cache[reference]
	return store, ok
}

var _ Resolver = (*URLPathResolver)(nil)
