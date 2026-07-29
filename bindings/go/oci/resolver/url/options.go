package url

import (
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/oci/cache"
)

// Option is an interface for configuring the CachingResolver.
type Option interface {
	Apply(*CachingResolver)
}

// OptionFunc is a function type that implements the Option interface.
type OptionFunc func(*CachingResolver)

func (f OptionFunc) Apply(resolver *CachingResolver) {
	f(resolver)
}

// WithBaseURL sets the base URL for the registry resolver.
// All references resolved will be under this base URL.
func WithBaseURL(baseURL string) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.baseURL = baseURL
	})
}

// WithBaseClient sets the base client to use for making requests to the registry.
// this also contains HTTP configuration.
func WithBaseClient(client remote.Client) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.baseClient = client
	})
}

// WithPlainHTTP sets whether to use plain HTTP instead of HTTPS for any repository clients
func WithPlainHTTP(plainHTTP bool) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.plainHTTP = plainHTTP
	})
}

// WithSubPath sets the repository prefix path used for the OCM repository.
// The OCM based artifacts will use this path as a repository prefix.
func WithSubPath(subPath string) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.subPath = subPath
	})
}

// WithBlobCache wires a manifest blob cache into the resolver. Stores
// returned by [CachingResolver.StoreForReference] are wrapped with
// [cache.Repository] so their Fetch consults the cache; layer blobs
// and other non-manifest media types pass through unchanged.
//
// Pass nil to disable blob caching (the default).
func WithBlobCache(blob *cache.BlobCache) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.SetBlobCache(blob)
	})
}

// WithReferenceCache wires a reference cache into the resolver.
// Stores returned by [CachingResolver.StoreForReference] are wrapped
// with [cache.Repository] so their Resolve consults the reference
// cache before performing a remote round-trip.
//
// Pass nil to disable reference caching (the default).
func WithReferenceCache(refs *cache.ReferenceCache) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.SetReferenceCache(refs)
	})
}
