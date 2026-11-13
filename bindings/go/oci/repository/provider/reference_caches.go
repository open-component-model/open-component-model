package provider

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sync/singleflight"

	"ocm.software/open-component-model/bindings/go/oci/cache"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

// ReferenceCaches holds one [cache.ReferenceCache] per repository scope. A reference cache owns
// its directory, so providers that use the same directory must share one ReferenceCaches,
// see [WithReferenceCaches].
type ReferenceCaches struct {
	tempDir string
	opts    cache.Options
	caches  sync.Map // scope → *cache.ReferenceCache
	init    singleflight.Group
}

// NewReferenceCaches returns reference caches that keep each scope in a subdirectory of
// opts.Dir. An empty opts.Dir means <tempDir>/ocm-oci-refcache, and an empty tempDir the
// directory of [os.TempDir].
func NewReferenceCaches(tempDir string, opts cache.Options) *ReferenceCaches {
	return &ReferenceCaches{tempDir: tempDir, opts: opts}
}

// get returns the reference cache for the repository of identity, creating it on first use.
// The scope is based solely on repository identity (host[:port]/path); credentials are
// excluded because short-lived tokens would create new scopes and kill cache reuse. Use
// [cache.RemotePolicyAlways] to require remote authorisation on every cache hit.
//
// Concurrent first-use callers for the same scope are collapsed so exactly one
// [cache.NewReferenceCache] runs. A nil r and a cache that fails to initialise return nil,
// which disables reference caching for the scope.
func (r *ReferenceCaches) get(identity *v1.OCIRegistryIdentity) *cache.ReferenceCache {
	if r == nil {
		return nil
	}
	scope := cache.RepositoryKey(identity)
	if v, ok := r.caches.Load(scope); ok {
		return v.(*cache.ReferenceCache)
	}

	v, err, _ := r.init.Do(scope, func() (any, error) {
		// Re-check under the singleflight so the leader picks up any
		// concurrently-published cache instead of building another.
		if existing, ok := r.caches.Load(scope); ok {
			return existing, nil
		}

		opts := r.opts
		if opts.Dir == "" {
			base := r.tempDir
			if base == "" {
				base = os.TempDir()
			}
			opts.Dir = filepath.Join(base, "ocm-oci-refcache", scope)
		} else {
			opts.Dir = filepath.Join(opts.Dir, scope)
		}

		c, err := cache.NewReferenceCache(opts)
		if err != nil {
			return nil, err
		}
		r.caches.Store(scope, c)
		return c, nil
	})
	if err != nil {
		slog.Warn("provider: failed to initialise reference cache for scope, continuing without caching",
			slog.String("scope", scope),
			slog.String("err", err.Error()))
		return nil
	}
	if v == nil {
		return nil
	}
	return v.(*cache.ReferenceCache)
}
