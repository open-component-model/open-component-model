package provider

import (
	"context"
	"fmt"
	"sync"

	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type cachedOCIDescriptors struct {
	identity  runtime.Identity
	manifests cache.OCIDescriptorCache
	layers    cache.OCIDescriptorCache
}

type ociCache struct {
	mu             sync.RWMutex
	ociDescriptors []cachedOCIDescriptors
	scheme         *runtime.Scheme
}

func (cache *ociCache) get(ctx context.Context, spec runtime.Typed) (manifests cache.OCIDescriptorCache, layers cache.OCIDescriptorCache, err error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	identity, err := GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, cache.scheme, spec)
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
	cache.ociDescriptors = append(cache.ociDescriptors)

	return entry.manifests, entry.layers, nil
}
