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

// WithBlobCache enables manifest Fetch caching for stores returned by
// [CachingResolver.StoreForReference], leaving other media types uncached.
//
// Pass nil to disable blob caching (the default).
func WithBlobCache(blob *cache.BlobCache) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.SetBlobCache(blob)
	})
}

// WithReferenceCache configures reference caching for stores returned by
// [CachingResolver.StoreForReference].
//
// Pass nil to disable reference caching (the default).
func WithReferenceCache(refs *cache.ReferenceCache) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.SetReferenceCache(refs)
	})
}

// WithChunkedPush enables chunked blob upload for remote repositories handed
// out by this resolver. chunkSize is the target PATCH chunk size in bytes;
// threshold is the minimum blob size for chunking to engage (blobs below it and
// all manifests use monolithic upload). A chunkSize <= 0 disables chunking; a
// threshold <= 0 uses remotestore.DefaultChunkThreshold.
//
// Chunked upload also enables streaming pushes (see remotestore.StreamingPusher)
// so blobs of unknown size or digest can be uploaded without buffering.
// Chunked and streaming pushes remain available with either cache enabled.
func WithChunkedPush(chunkSize, threshold int64) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.chunkSize = chunkSize
		resolver.chunkThreshold = threshold
	})
}
