// incremental_discovery.go - Prototype for incremental graph discovery in OCM.
//
// Key Idea: Cache resolved descriptors and only re-traverse updated components.
// This reduces redundant work for large graphs with infrequent updates.

package prototypes

import (
	"context"
	"sync"
	"time"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
)

// IncrementalDiscoverer implements incremental graph discovery by caching
// resolved descriptors and only re-traversing updated components.
type IncrementalDiscoverer struct {
	mu               sync.Mutex
	descriptorCache  map[string]*descriptor.Descriptor // Key: "component:version"
	lastUpdatedCache map[string]time.Time                  // Key: "component:version"
	resolver         resolvers.ComponentVersionRepositoryResolver
}

// NewIncrementalDiscoverer creates a new IncrementalDiscoverer.
func NewIncrementalDiscoverer(resolver resolvers.ComponentVersionRepositoryResolver) *IncrementalDiscoverer {
	return &IncrementalDiscoverer{
		descriptorCache:  make(map[string]*descriptor.Descriptor),
		lastUpdatedCache: make(map[string]time.Time),
		resolver:         resolver,
	}
}

// Discover resolves a component and its dependencies, but only re-traverses
// components that have been updated since the last discovery.
func (d *IncrementalDiscoverer) Discover(
	ctx context.Context,
	key string,
	recursive bool,
) (*descriptor.Descriptor, error) {
	d.mu.Lock()
	cachedDesc, exists := d.descriptorCache[key]
	lastUpdated, _ := d.lastUpdatedCache[key]
	d.mu.Unlock()

	// If the component is cached and not updated, return the cached descriptor.
	if exists && !d.isUpdated(ctx, key, lastUpdated) {
		return cachedDesc, nil
	}

	// Resolve the component from the source.
	desc, err := d.resolveComponent(ctx, key)
	if err != nil {
		return nil, err
	}

	// Cache the descriptor and update the timestamp.
	d.mu.Lock()
	d.descriptorCache[key] = desc
	d.lastUpdatedCache[key] = time.Now()
	d.mu.Unlock()

	// Recursively discover dependencies if enabled.
	if recursive {
		for _, ref := range desc.Component.References {
			childKey := ref.Component + ":" + ref.Version
			if _, err := d.Discover(ctx, childKey, true); err != nil {
				return nil, err
			}
		}
	}

	return desc, nil
}

// resolveComponent resolves a component from the source repository.
func (d *IncrementalDiscoverer) resolveComponent(
	ctx context.Context,
	key string,
) (*descriptor.Descriptor, error) {
	// TODO: Implement resolution logic (reuse multiResolver from OCM).
	return nil, nil
}

// isUpdated checks if a component has been updated since the last discovery.
func (d *IncrementalDiscoverer) isUpdated(
	ctx context.Context,
	key string,
	lastUpdated time.Time,
) bool {
	// TODO: Implement logic to check for updates (e.g., timestamp, hash).
	return false
}

// ClearCache clears the descriptor and timestamp caches.
func (d *IncrementalDiscoverer) ClearCache() {
	d.mu.Lock()
	d.descriptorCache = make(map[string]*descriptor.Descriptor)
	d.lastUpdatedCache = make(map[string]time.Time)
	d.mu.Unlock()
}