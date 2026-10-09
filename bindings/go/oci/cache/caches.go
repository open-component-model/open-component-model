package cache

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/singleflight"

	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

// Caches owns the blob cache and the per-scope reference caches of one cache directory.
// A cache instance assumes exclusive ownership of its on-disk files, so everything that
// resolves to the same directory must share one Caches.
type Caches struct {
	// tempDir is the base directory used when the cache options carry no Dir.
	tempDir string

	// blobOpts, when non-nil, enables a shared content-addressable blob
	// cache. All credential scopes share one BlobCache because blobs are
	// immutable and identified by digest — a digest unambiguously identifies
	// content regardless of who fetched it. Only tag→digest resolution is
	// access-controlled; once you hold a digest you are authorised.
	//
	// Note: while an attacker who *guesses* a digest could observe its
	// existence in the cache, they must first obtain the digest via a
	// [ReferenceCache.Resolve] under credentials that entitle them
	// to see the tag — which IS credential-scoped. When we grow the
	// blob cache to cover resource layers, callers with weaker
	// credentials must not be able to learn digests from a stronger
	// scope, so revisit this decision if the cache starts holding
	// layer blobs (see open-component-model#2833 discussion).
	blobOpts *Options

	// referenceOpts, when non-nil, enables per-scope reference caches.
	// Tag resolution IS access-controlled (a private registry won't return a
	// descriptor for a tag you can't read), so each credential scope gets its
	// own ReferenceCache to prevent one scope from reading tag mappings
	// resolved under a different credential set.
	referenceOpts *Options

	// blob is the single BlobCache shared across all credential scopes.
	// Initialised lazily on first use, guarded by blobInit.
	blob     *BlobCache
	blobInit sync.Once

	// closed turns every accessor into a no-op that reports caching as disabled.
	closed atomic.Bool

	// references stores one *ReferenceCache per credential scope key.
	references sync.Map // string → *ReferenceCache

	// referenceInit serialises the constructor calls per scope
	// key so a burst of first-use callers for the same scope does not
	// each build (and discard) their own [ReferenceCache]. The
	// winner is published into references; every other caller
	// finds it there via Load without re-running the (expensive)
	// on-disk reseed. Mirrors the pattern used by storeCache.loadOrStore
	// (see open-component-model/ocm-project#694).
	referenceInit singleflight.Group
}

// NewCaches creates the caches rooted at tempDir (the OS temp directory when empty).
// Pass [Options]{} for sane defaults; a nil options value disables that cache.
// The directories default to <tempDir>/ocm-oci-cas and <tempDir>/ocm-oci-refcache and
// are deterministic, so a later process over the same tempDir reuses the on-disk entries.
func NewCaches(tempDir string, blobOpts, referenceOpts *Options) *Caches {
	return &Caches{
		tempDir:       tempDir,
		blobOpts:      blobOpts,
		referenceOpts: referenceOpts,
	}
}

// Blob returns the shared blob cache, or nil when blob caching is disabled, failed to
// initialise, or the Caches were closed.
func (c *Caches) Blob() *BlobCache {
	if c.closed.Load() {
		return nil
	}
	c.blobInit.Do(func() { c.blob = c.newBlobCache() })
	return c.blob
}

// Close stops the background expiry of every cache built so far and disables the Caches.
// Files stay on disk so a later process over the same directory reuses them.
func (c *Caches) Close() {
	if c.closed.Swap(true) {
		return
	}
	// Consuming the once waits for an in-flight build and prevents a later one.
	c.blobInit.Do(func() {})
	if c.blob != nil {
		c.blob.Close()
	}
	c.references.Range(func(_, v any) bool {
		v.(*ReferenceCache).Close()
		return true
	})
}

func (c *Caches) baseDir() string {
	if c.tempDir == "" {
		return os.TempDir()
	}
	return c.tempDir
}

// newBlobCache is the once-only constructor for the shared blob cache.
func (c *Caches) newBlobCache() *BlobCache {
	if c.blobOpts == nil {
		return nil
	}
	opts := *c.blobOpts
	if opts.Dir == "" {
		opts.Dir = filepath.Join(c.baseDir(), "ocm-oci-cas")
	}
	bc, err := NewBlobCache(opts)
	if err != nil {
		slog.Warn("cache: failed to initialise shared blob cache, continuing without caching",
			slog.String("err", err.Error()))
		return nil
	}
	return bc
}

// Reference returns the ReferenceCache for the given repository scope, or nil when
// reference caching is disabled or the Caches were closed. The cache is created and persisted on first use. The scope is based solely on
// repository identity (host[:port]/path); credentials are excluded because
// short-lived tokens would create new scopes and kill cache reuse. Use
// [RemotePolicyAlways] to require remote authorisation on every cache hit.
//
// Concurrent first-use callers for the same scope are collapsed via
// [singleflight.Group] so exactly one [NewReferenceCache] runs.
func (c *Caches) Reference(identity *identityv1.OCIRegistryIdentity) *ReferenceCache {
	if c.referenceOpts == nil || c.closed.Load() {
		return nil
	}
	scope := RepositoryKey(identity)
	if v, ok := c.references.Load(scope); ok {
		return v.(*ReferenceCache)
	}

	v, err, _ := c.referenceInit.Do(scope, func() (any, error) {
		// Re-check under the singleflight so the leader picks up any
		// concurrently-published cache instead of building another.
		if existing, ok := c.references.Load(scope); ok {
			return existing, nil
		}

		opts := *c.referenceOpts
		if opts.Dir == "" {
			opts.Dir = filepath.Join(c.baseDir(), "ocm-oci-refcache", scope)
		} else {
			opts.Dir = filepath.Join(opts.Dir, scope)
		}

		rc, err := NewReferenceCache(opts)
		if err != nil {
			return nil, err
		}
		c.references.Store(scope, rc)
		// Close may have ranged over references before this one was stored.
		if c.closed.Load() {
			rc.Close()
		}
		return rc, nil
	})
	if err != nil {
		slog.Warn("cache: failed to initialise reference cache for scope, continuing without caching",
			slog.String("scope", scope),
			slog.String("err", err.Error()))
		return nil
	}
	if v == nil {
		return nil
	}
	return v.(*ReferenceCache)
}
