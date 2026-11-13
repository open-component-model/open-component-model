package provider

import (
	"time"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	ocicachev1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Options holds configuration options for the OCI repository provider.
type Options struct {
	// TempDir is the shared default temporary filesystem directory for any
	// temporary data created by the repositories provided by the provider.
	TempDir string

	// UserAgent identifies the caller and is applied in two distinct ways:
	//   1. Transport-level: injected as the HTTP User-Agent header on every
	//      outbound request via the internal ocmhttp client.
	//   2. Auth-layer: injected as the User-Agent header on authenticated OCI
	//      requests via auth.Client.Header.
	//   3. OCM annotation: written as AnnotationOCMCreator on component versions
	//      added via AddComponentVersion.
	// Defaults to DefaultCreator when empty.
	UserAgent string

	// Scheme is the runtime scheme used by the repositories.
	Scheme *runtime.Scheme

	// HTTPConfig is the HTTP client configuration (timeouts, per-host overrides)
	// used to build the provider's internal HTTP client. When nil, default
	// transport timeouts and oras-go's retry behaviour are used.
	// Accepts the serialisable config type so that external plugins can
	// round-trip it over the wire and reconstruct an equivalent client.
	HTTPConfig *httpv1alpha1.Config

	// BlobCacheOptions, when non-nil, configures the manifest blob cache
	// the provider builds and shares across every OCI repository it hands
	// out. The zero value [cache.Options]{} is sufficient for sane
	// defaults — only Dir is auto-derived from TempDir when left empty.
	// Leave nil to disable blob caching unless CacheConfig enables it.
	BlobCacheOptions *cache.Options

	// ReferenceCacheOptions, when non-nil, configures the reference
	// cache the provider builds and shares across every OCI repository
	// it hands out. The zero value [cache.Options]{} is sufficient for
	// sane defaults — only Dir is auto-derived from TempDir when left
	// empty. Leave nil to disable reference caching unless CacheConfig
	// enables it.
	ReferenceCacheOptions *cache.Options

	// CacheConfig, when non-nil, is applied on top of BlobCacheOptions and
	// ReferenceCacheOptions. Its enabled fields turn a cache on or off.
	CacheConfig *ocicachev1alpha1.Config

	// BlobCache, when non-nil, is used instead of building a blob cache from
	// BlobCacheOptions, so callers that create many providers can share one.
	BlobCache *cache.BlobCache

	// ReferenceCaches, when non-nil, is used instead of building reference caches from
	// ReferenceCacheOptions, so callers that create many providers can share them.
	ReferenceCaches *ReferenceCaches
}

type Option func(*Options)

// WithTempDir sets the temporary directory option
func WithTempDir(dir string) Option {
	return func(o *Options) {
		o.TempDir = dir
	}
}

// WithUserAgent sets the caller identity string used in three places: the
// transport-level User-Agent header on every HTTP request, the auth-layer
// User-Agent header on authenticated OCI requests, and the AnnotationOCMCreator
// annotation on newly added component versions. Defaults to DefaultCreator.
func WithUserAgent(userAgent string) Option {
	return func(o *Options) {
		o.UserAgent = userAgent
	}
}

// WithScheme sets the runtime scheme option
func WithScheme(scheme *runtime.Scheme) Option {
	return func(o *Options) {
		o.Scheme = scheme
	}
}

// WithHTTPConfig sets the HTTP client configuration used for OCI registry
// traffic. The provider builds its internal client from cfg on construction,
// applying timeouts and per-host overrides. When nil, default transport
// timeouts and oras-go's retry behaviour are used.
func WithHTTPConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.HTTPConfig = cfg
	}
}

// WithBlobCacheOptions enables a shared manifest blob cache across every
// OCI repository the provider hands out. Pass [cache.Options]{} for
// sane defaults; pass nil (the default) to disable caching. The cache
// directory defaults to <TempDir>/ocm-oci-cas and is deterministic
// so consecutive provider instances over the same TempDir reuse the
// existing on-disk entries.
func WithBlobCacheOptions(opts *cache.Options) Option {
	return func(o *Options) {
		o.BlobCacheOptions = opts
	}
}

// WithOCICacheConfig applies cfg to the blob and the reference cache. The options of
// [WithBlobCacheOptions] and [WithReferenceCacheOptions] are the defaults it applies to;
// a cache without options is off unless cfg enables it. cfg only turns off a cache set with
// [WithBlobCache] or [WithReferenceCaches].
func WithOCICacheConfig(cfg *ocicachev1alpha1.Config) Option {
	return func(o *Options) {
		o.CacheConfig = cfg
	}
}

// WithBlobCache makes the provider use bc instead of building its own blob cache.
// It takes precedence over [WithBlobCacheOptions]. The caller owns bc.
func WithBlobCache(bc *cache.BlobCache) Option {
	return func(o *Options) {
		o.BlobCache = bc
	}
}

// WithReferenceCaches makes the provider use rc instead of building its own reference caches.
// It takes precedence over [WithReferenceCacheOptions].
func WithReferenceCaches(rc *ReferenceCaches) Option {
	return func(o *Options) {
		o.ReferenceCaches = rc
	}
}

// WithReferenceCacheOptions enables a shared reference cache across
// every OCI repository the provider hands out. Pass [cache.Options]{}
// for sane defaults; pass nil (the default) to disable. The cache
// directory defaults to <TempDir>/ocm-oci-refcache and is
// deterministic so consecutive provider instances over the same
// TempDir reuse the existing on-disk snapshot.
func WithReferenceCacheOptions(opts *cache.Options) Option {
	return func(o *Options) {
		o.ReferenceCacheOptions = opts
	}
}

// applyBlobCacheConfig returns the blob cache options with cfg applied, or nil if the blob
// cache is off. opts are the defaults and are copied, so options shared between providers are
// not mutated.
func applyBlobCacheConfig(opts *cache.Options, cfg *ocicachev1alpha1.Config) *cache.Options {
	applied, ok := enabledCopy(opts, cfg.BlobEnabled(opts != nil))
	if !ok {
		return nil
	}
	if cfg == nil {
		return applied
	}
	applyRemotePolicy(applied, cfg)
	if b := cfg.Blob; b != nil {
		if b.TTL != nil {
			applied.TTL = time.Duration(*b.TTL)
		}
		if b.MaxBlobSize != nil {
			applied.MaxBlobSize = *b.MaxBlobSize
		}
		if b.MaxTotalSize != nil {
			applied.MaxTotalSize = *b.MaxTotalSize
		}
	}
	return applied
}

// applyReferenceCacheConfig is [applyBlobCacheConfig] for the reference cache.
func applyReferenceCacheConfig(opts *cache.Options, cfg *ocicachev1alpha1.Config) *cache.Options {
	applied, ok := enabledCopy(opts, cfg.ReferenceEnabled(opts != nil))
	if !ok {
		return nil
	}
	if cfg == nil {
		return applied
	}
	applyRemotePolicy(applied, cfg)
	if r := cfg.Reference; r != nil && r.TTL != nil {
		applied.TTL = time.Duration(*r.TTL)
	}
	return applied
}

// enabledCopy returns a copy of opts, or the zero options if opts is nil, when enabled.
func enabledCopy(opts *cache.Options, enabled bool) (*cache.Options, bool) {
	if !enabled {
		return nil, false
	}
	var applied cache.Options
	if opts != nil {
		applied = *opts
	}
	return &applied, true
}

func applyRemotePolicy(opts *cache.Options, cfg *ocicachev1alpha1.Config) {
	if cfg.RemotePolicy != "" {
		opts.RemotePolicy = cache.RemotePolicy(cfg.RemotePolicy)
	}
}
