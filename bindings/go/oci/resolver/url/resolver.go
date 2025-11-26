package url

import (
	"context"
	"fmt"
	"sync"

	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/path"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func New(opts ...Option) (*CachingResolver, error) {
	resolver := &CachingResolver{}
	for _, opt := range opts {
		opt.Apply(resolver)
	}

	if resolver.baseURL == "" {
		return nil, fmt.Errorf("base URL must be set")
	}

	return resolver, nil
}

// CachingResolver is a Resolver that resolves references to URLs for Component Versions and Resources.
// It uses a baseURL and a baseClient to get a remote store for a reference.
// each repository is only created once per reference.
type CachingResolver struct {
	baseURL    string
	subPath    string
	baseClient remote.Client
	plainHTTP  bool

	DisableCacheProxy bool

	cacheMu sync.RWMutex
	cache   map[string]spec.Store
}

func (resolver *CachingResolver) SetClient(client remote.Client) {
	resolver.baseClient = client
}

func (resolver *CachingResolver) BasePath() string {
	basePath := resolver.baseURL
	if resolver.subPath != "" {
		basePath = basePath + "/" + resolver.subPath
	}
	return basePath + "/" + path.DefaultComponentDescriptorPath
}

func (resolver *CachingResolver) ComponentVersionReference(ctx context.Context, component, version string) string {
	tag := oci.LooseSemverToOCITag(ctx, version) // Remove prohibited characters.
	return fmt.Sprintf("%s/%s:%s", resolver.BasePath(), component, tag)
}

func (resolver *CachingResolver) Reference(reference string) (fmt.Stringer, error) {
	return registry.ParseReference(reference)
}

// Ping checks registry availability and validates authentication credentials.
// It extracts the host (hostname:port) from baseURL and calls ORAS registry.Ping on the /v2/ endpoint.
// The subPath is ignored as ORAS only supports pinging the registry root.
func (resolver *CachingResolver) Ping(ctx context.Context) error {
	// Parse baseURL (with or without scheme) and extract host
	parsedURL, err := runtime.ParseURLAndAllowNoScheme(resolver.baseURL)
	if err != nil {
		return fmt.Errorf("failed to parse base URL: %w", err)
	}

	// Use only host (hostname:port) for registry ping (discard scheme and path)
	r, err := remote.NewRegistry(parsedURL.Host)
	if err != nil {
		return fmt.Errorf("failed to create registry client: %w", err)
	}
	r.PlainHTTP = resolver.plainHTTP
	if resolver.baseClient != nil {
		r.Client = resolver.baseClient
	}
	if err := r.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping registry: %w", err)
	}
	return nil
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

	repo := &remote.Repository{
		Reference: ref,
		// to remain fully compatible with all OCI repositories, we MUST skip referrers GC.
		// this is because most "classic" OCI repositories such as Docker or GHCR that were
		// developed before the referrers API ALSO do not provide delete support for manifests.
		// see https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#deleting-manifests
		//
		// This means that by default, we cannot delete referrers from the repository.
		// This is a limitation of the OCI distribution spec implementors and not specific to this resolver.
		SkipReferrersGC: true,
	}

	repo.PlainHTTP = resolver.plainHTTP
	if resolver.baseClient != nil {
		repo.Client = resolver.baseClient
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
