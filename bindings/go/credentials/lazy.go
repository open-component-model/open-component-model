package credentials

import (
	"context"
	"sync/atomic"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// LazyResolver is a Resolver whose target is supplied after construction.
//
// It exists because credential resolution and plugin registration depend on
// each other: a plugin contributes the credential types ToGraph needs while
// ingesting a configuration, so the plugin must be built before the graph,
// while resolving a credential needs the graph. Setting the graph on an
// already-registered plugin breaks that cycle.
//
// A LazyResolver belongs to one plugin manager, is set once while it is wired
// and read afterwards, so the value it holds is that manager's own graph: the
// controller builds a manager and a graph per request, and neither outlives
// the other.
//
// Until Set is called, Resolve reports ErrNotFound, so a caller wired without
// a graph resolves no credentials rather than failing.
type LazyResolver struct {
	resolver atomic.Pointer[Resolver]
}

var _ Resolver = (*LazyResolver)(nil)

// Set makes r the resolver every later Resolve delegates to. A nil r leaves
// the LazyResolver unset.
func (l *LazyResolver) Set(r Resolver) {
	if r == nil {
		return
	}
	l.resolver.Store(&r)
}

// Resolve delegates to the resolver Set supplied, reporting ErrNotFound while
// there is none.
func (l *LazyResolver) Resolve(ctx context.Context, identity runtime.Identity) (runtime.Typed, error) {
	resolver := l.resolver.Load()
	if resolver == nil {
		return nil, ErrNotFound
	}
	return (*resolver).Resolve(ctx, identity)
}
