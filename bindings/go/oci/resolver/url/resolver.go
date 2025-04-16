package url

import (
	"context"
	"fmt"
	"sync"

	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/path"
)

func New(baseURL string) *CachingResolver {
	return &CachingResolver{
		BaseURL: baseURL,
	}
}

// CachingResolver is a Resolver that resolves references to URLs for Component Versions and Resources.
// It uses a BaseURL and a BaseClient to get a remote store for a reference.
// each repository is only created once per reference.
type CachingResolver struct {
	BaseURL    string
	BaseClient remote.Client
	PlainHTTP  bool

	DisableCacheProxy bool

	cacheMu sync.RWMutex
	cache   map[string]spec.Store
}

func (resolver *CachingResolver) SetClient(client remote.Client) {
	resolver.BaseClient = client
}

func (resolver *CachingResolver) BasePath() string {
	return resolver.BaseURL + "/" + path.DefaultComponentDescriptorPath
}

func (resolver *CachingResolver) ComponentVersionReference(component, version string) string {
	return fmt.Sprintf("%s/%s:%s", resolver.BasePath(), component, version)
}

func (resolver *CachingResolver) Reference(reference string) (fmt.Stringer, error) {
	return registry.ParseReference(reference)
}

func (resolver *CachingResolver) StoreForReference(_ context.Context, reference string) (spec.Store, error) {
	rawRef, err := resolver.Reference(reference)
	if err != nil {
		return nil, err
	}
	ref := rawRef.(registry.Reference)
	key := fmt.Sprintf("%s/%s", ref.Registry, ref.Repository)

	if store, ok := resolver.getFromCache(key); ok {
		return store, nil
	}

	repo := &remote.Repository{Reference: ref}

	repo.PlainHTTP = resolver.PlainHTTP
	if resolver.BaseClient != nil {
		repo.Client = resolver.BaseClient
	}

	resolver.addToCache(key, repo)

	return repo, nil
}

func (resolver *CachingResolver) addToCache(reference string, store spec.Store) {
	resolver.cacheMu.Lock()
	defer resolver.cacheMu.Unlock()
	if resolver.cache == nil {
		resolver.cache = make(map[string]spec.Store)
	}
	resolver.cache[reference] = store
}

func (resolver *CachingResolver) getFromCache(reference string) (spec.Store, bool) {
	resolver.cacheMu.RLock()
	defer resolver.cacheMu.RUnlock()
	store, ok := resolver.cache[reference]
	return store, ok
}
