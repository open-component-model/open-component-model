package sync

import (
	"cmp"
	"context"
)

// GraphDiscovererOptions configures how a GraphDiscoverer operates.
// It defines the entry points (roots) and the logic for resolving and
// discovering neighbors.
//
// Generics:
//   - K: comparable key type for vertices (e.g., string, int).
//   - V: value type associated with each vertex (e.g., a struct or object).
//
// TODO(fabianburth): Add a recursion depth limit.
// TODO(jakobmoellerdev): add queue/workerpool to introduce a discovery
// concurrency limit (https://github.com/open-component-model/ocm-project/issues/705)
type GraphDiscovererOptions[K cmp.Ordered, V any] struct {
	// Roots is the set of starting vertex keys for discovery.
	// Discovery will begin from these vertices and expand recursively.
	Roots []K

	// Resolver maps a vertex key (K) to its resolved value (V).
	// This is typically where external I/O occurs, e.g. fetching a resource.
	// Must be concurrency-safe.
	Resolver Resolver[K, V]

	// Discoverer maps a resolved value (V) to its child vertex keys ([]K).
	// This controls how the graph is expanded once a vertex is resolved.
	// Must be concurrency-safe.
	Discoverer Discoverer[K, V]
}

// Resolver defines how to resolve a vertex key into its value.
type Resolver[K cmp.Ordered, V any] interface {
	// Resolve takes a key and returns its resolved value or an error.
	// Must be safe to call concurrently.
	Resolve(ctx context.Context, key K) (value V, err error)
}

// ResolverFunc is an adapter to use a function as a Resolver.
type ResolverFunc[K cmp.Ordered, V any] func(ctx context.Context, key K) (value V, err error)

// Resolve calls the underlying function.
func (f ResolverFunc[K, V]) Resolve(ctx context.Context, key K) (value V, err error) {
	return f(ctx, key)
}

// Discoverer defines how to expand a vertex into its child keys.
type Discoverer[K cmp.Ordered, V any] interface {
	// Discover takes a resolved value and returns the keys of its neighbors.
	// Must be safe to call concurrently.
	Discover(ctx context.Context, parent V) (children []K, err error)
}

// DiscovererFunc is an adapter to use a function as a Discoverer.
type DiscovererFunc[K cmp.Ordered, V any] func(ctx context.Context, parent V) (children []K, err error)

// Discover calls the underlying function.
func (f DiscovererFunc[K, V]) Discover(ctx context.Context, parent V) (children []K, err error) {
	return f(ctx, parent)
}
