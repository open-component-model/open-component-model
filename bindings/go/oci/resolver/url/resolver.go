package url

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/oras-project/oras-go/v3/registry/remote"
	"github.com/oras-project/oras-go/v3/registry/remote/errcode"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
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
	// baseScheme is the scheme prefix the base URL was given with, "" for
	// none. It is deprecated input: see schemeFor and plainHTTPFor.
	baseScheme string
	// plainHTTP is the programmatic default for hosts the HTTP configuration
	// does not decide. See WithPlainHTTP.
	plainHTTP bool
	// httpConfig is the central HTTP client configuration, if any.
	httpConfig *httpv1alpha1.Config

	DisableCacheProxy bool

	// chunkSize and chunkThreshold configure chunked blob upload on the
	// RemoteStores this resolver hands out. See WithChunkedPush. Zero disables
	// chunking (monolithic push).
	chunkSize      int64
	chunkThreshold int64

	cacheMu sync.RWMutex
	cache   map[string]*remotestore.RemoteStore

	// blobCache is configured via [WithBlobCache] or
	// [CachingResolver.SetBlobCache].
	//
	// Stored in an atomic.Pointer so [CachingResolver.SetBlobCache] can
	// race with concurrent [CachingResolver.StoreForReference] calls
	// without a data race — the doc contract of SetBlobCache says only
	// stores handed out after the call must observe the new cache, so
	// tearing is not tolerable.
	blobCache atomic.Pointer[cache.BlobCache]

	// referenceCache is configured via [WithReferenceCache] or
	// [CachingResolver.SetReferenceCache], with the same concurrency
	// contract as blobCache.
	referenceCache atomic.Pointer[cache.ReferenceCache]
}

// SetBlobCache configures manifest caching for subsequently returned stores,
// including resolver-cache hits; see [WithBlobCache].
func (resolver *CachingResolver) SetBlobCache(c *cache.BlobCache) {
	resolver.blobCache.Store(c)
}

// SetReferenceCache configures reference caching for subsequently returned
// stores; see [WithReferenceCache].
func (resolver *CachingResolver) SetReferenceCache(c *cache.ReferenceCache) {
	resolver.referenceCache.Store(c)
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
	tag, err := oci.VersionToOCITag(ctx, version)
	if err != nil {
		slog.WarnContext(ctx, "building component version reference with an invalid OCI tag", "component", component, "version", version, "error", err)
	}
	return fmt.Sprintf("%s/%s:%s", resolver.BasePath(), component, tag)
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
	// Per-host settings are keyed by the host the request goes to, which for docker.io is registry-1.docker.io.
	r.PlainHTTP, err = resolver.plainHTTPFor(ctx, resolver.schemeFor(parsedURL.Scheme, parsedURL.Host), r.Reference.Host())
	if err != nil {
		return fmt.Errorf("failed to determine plain HTTP setting for host %s: %w", r.Reference.Host(), err)
	}
	if resolver.baseClient != nil {
		r.Client = resolver.baseClient
	}
	if err := r.Ping(ctx); err != nil {
		errResp := &errcode.ErrorResponse{}
		if ok := errors.As(err, &errResp); ok {
			switch errResp.StatusCode {
			case http.StatusForbidden, http.StatusUnauthorized:
				// According to the distribution spec the registry might opt for credentials
				// for pinging the Base. We consider that as a success, because at least the
				// registry responded and is available regardless of access or configured credentials.
				// References:
				//   - https://distribution.github.io/distribution/spec/api/#base
				return nil
			}
		}

		return fmt.Errorf("failed to ping registry: %w", err)
	}
	return nil
}

func (resolver *CachingResolver) StoreForReference(ctx context.Context, reference string) (spec.Store, error) {
	ref, err := looseref.ParseReference(reference)
	if err != nil {
		return nil, err
	}

	key := fmt.Sprintf("%s/%s", ref.Registry, ref.Repository)
	if ref.Scheme != "" {
		key = fmt.Sprintf("%s://%s", ref.Scheme, key)
	}

	if remoteStore, ok := resolver.getFromCache(key); ok {
		return cache.Wrap(remoteStore, resolver.blobCache.Load(), resolver.referenceCache.Load()), nil
	}

	plainHTTP, err := resolver.plainHTTPFor(ctx, resolver.schemeFor(ref.Scheme, ref.Registry), ref.Host())
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %q: %w", reference, err)
	}

	// The store addresses the repository; tag and digest are passed per call.
	repo, err := remote.NewRepository(ref.Registry + "/" + ref.Repository)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository for %q: %w", reference, err)
	}
	// to remain fully compatible with all OCI repositories, we MUST skip referrers GC.
	// this is because most "classic" OCI repositories such as Docker or GHCR that were
	// developed before the referrers API ALSO do not provide delete support for manifests.
	// see https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#deleting-manifests
	//
	// This means that by default, we cannot delete referrers from the repository.
	// This is a limitation of the OCI distribution spec implementors and not specific to this resolver.
	repo.SkipReferrersGC = true
	repo.Registry.PlainHTTP = plainHTTP

	if resolver.baseClient != nil {
		repo.Registry.Client = resolver.baseClient
	}

	store := &remotestore.RemoteStore{
		Repository:     repo,
		ChunkSize:      resolver.chunkSize,
		ChunkThreshold: resolver.chunkThreshold,
	}
	resolver.addToCache(key, store)

	return cache.Wrap(store, resolver.blobCache.Load(), resolver.referenceCache.Load()), nil
}

// plainHTTPFor decides whether host is reached over cleartext HTTP by the
// shared rule in ocmhttp.PlainHTTPFor, with WithPlainHTTP as the default for
// hosts the HTTP configuration does not decide.
//
// A scheme prefix on the reference or the base URL is deprecated input: it
// never changes the connection and only logs a warning that points at the
// migration guide before the shared rule checks it.
func (resolver *CachingResolver) plainHTTPFor(ctx context.Context, scheme, host string) (bool, error) {
	if scheme != "" {
		slog.WarnContext(ctx, "the scheme prefix on a registry URL is deprecated; drop it and set plainHTTP in the HTTP client configuration for hosts without TLS",
			slog.String("host", host), slog.String("scheme", scheme), slog.String("url", httpPrefixMigrationGuide))
	}
	plain, err := ocmhttp.PlainHTTPFor(ctx, resolver.httpConfig, host, scheme, resolver.plainHTTP)
	if err != nil {
		return false, fmt.Errorf("%w and drop the prefix (see %s)", err, httpPrefixMigrationGuide)
	}
	return plain, nil
}

// schemeFor returns the scheme prefix that applies to a reference on
// registry: its own scheme, else the base URL's scheme when registry is the
// base host, else none. A reference on another registry, such as a resource
// on ghcr.io next to an http:// base URL, must not inherit the base scheme.
func (resolver *CachingResolver) schemeFor(scheme, registry string) string {
	if scheme != "" {
		return scheme
	}
	base, err := runtime.ParseURLAndAllowNoScheme(resolver.baseURL)
	if err != nil || base.Host != registry {
		return ""
	}
	return resolver.baseScheme
}

// httpPrefixMigrationGuide explains how to replace a scheme prefix with
// plainHTTP in the HTTP client configuration.
const httpPrefixMigrationGuide = "https://ocm.software/docs/changelog/migrate-from-http-prefixes/"

func (resolver *CachingResolver) addToCache(reference string, store *remotestore.RemoteStore) {
	resolver.cacheMu.Lock()
	defer resolver.cacheMu.Unlock()
	if resolver.cache == nil {
		resolver.cache = make(map[string]*remotestore.RemoteStore)
	}
	resolver.cache[reference] = store
}

func (resolver *CachingResolver) getFromCache(reference string) (*remotestore.RemoteStore, bool) {
	resolver.cacheMu.RLock()
	defer resolver.cacheMu.RUnlock()
	store, ok := resolver.cache[reference]
	return store, ok
}
