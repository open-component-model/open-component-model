package provider

import (
	"context"
	"fmt"
	"sync"

	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// cachedOCIDescriptors represents a set of OCI descriptor caches for a specific repository identity.
// It maintains separate caches for manifests and layers to optimize different types of OCI operations.
type cachedOCIDescriptors struct {
	identity  runtime.Identity
	manifests cache.OCIDescriptorCache
	layers    cache.OCIDescriptorCache
}

// ociCache provides a thread-safe cache for OCI descriptors.
// It maintains separate caches for different repository identities and supports
// both manifest and layer caching for improved performance.
type ociCache struct {
	mu             sync.RWMutex
	ociDescriptors []cachedOCIDescriptors
	scheme         *runtime.Scheme
}

// get retrieves or creates OCI descriptor caches for a given repository specification.
// It ensures thread-safe access to the cache and creates new in-memory caches
// for new repository identities.
func (cache *ociCache) get(ctx context.Context, spec runtime.Typed) (manifests cache.OCIDescriptorCache, layers cache.OCIDescriptorCache, err error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	identity, err := getCacheIdentity(ctx, cache.scheme, spec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get identity from OCI repository: %w", err)
	}

	for _, entry := range cache.ociDescriptors {
		if identity.Match(entry.identity, runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			return entry.manifests, entry.layers, nil
		}
	}

	entry := cachedOCIDescriptors{
		identity:  identity,
		manifests: inmemory.New(),
		layers:    inmemory.New(),
	}
	cache.ociDescriptors = append(cache.ociDescriptors, entry)

	return entry.manifests, entry.layers, nil
}
