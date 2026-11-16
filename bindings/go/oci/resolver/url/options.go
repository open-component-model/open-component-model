package url

import (
	"github.com/oras-project/oras-go/v3/registry/remote"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
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

// WithPlainHTTP uses cleartext HTTP for every host the HTTP configuration
// does not decide; an explicit plainHTTP value for the host in the
// configuration wins. It is the programmatic counterpart of a global
// plainHTTP: true, meant for tools and tests that talk to a local registry.
// User-facing input goes through the HTTP configuration instead.
func WithPlainHTTP(plainHTTP bool) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.plainHTTP = plainHTTP
	})
}

// WithBaseURLScheme records the scheme prefix ("http" or "https") the base
// URL was given with, so the resolver can check it against the HTTP
// configuration. The prefix is deprecated and never decides the connection:
// http:// is an error for a host that is not plainHTTP, https:// on a
// plainHTTP host warns about the downgrade. Pass "" for no prefix.
func WithBaseURLScheme(scheme string) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.baseScheme = scheme
	})
}

// WithHTTPConfig sets the central HTTP client configuration. A nil config is
// allowed.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return OptionFunc(func(resolver *CachingResolver) {
		resolver.httpConfig = cfg
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
