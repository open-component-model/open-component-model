package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/singleflight"
	"oras.land/oras-go/v2/registry"
)

// referenceSubdir is the directory under [Options.Dir] that the
// reference cache owns. Each namespace gets its own JSON file inside
// it, named after the hex-encoded SHA-256 of the namespace string.
// Splitting per namespace means an [ReferenceCache.Add] only rewrites
// the file for the affected namespace and namespaces never compete for
// the same snapshot file. The layout mirrors [BlobCache]'s
// `<Dir>/blobs/...` scoping.
//
// A cryptographic digest makes filename collisions between distinct
// namespaces infeasible in practice, so two namespaces can never
// clobber each other's snapshot file. The canonical namespace string
// is still written into the snapshot body so a reseed recovers the
// exact key without trusting the filename to preserve it.
const referenceSubdir = "refs"

// referenceFileExt is appended to the hashed namespace to form the
// per-namespace snapshot filename.
const referenceFileExt = ".json"

// referenceEntry is the shared shape of a cached resolve, used both
// as the on-disk record and as the in-memory LRU value. SavedAt
// records when the entry was first resolved so a reseed can honour
// [Options.TTL] instead of resurrecting arbitrarily stale mappings
// with a fresh in-memory expiry.
type referenceEntry struct {
	Descriptor ociImageSpecV1.Descriptor `json:"descriptor"`
	SavedAt    time.Time                 `json:"savedAt"`
}

// referenceFileSnapshot is the on-disk shape of a single namespace's
// portion of [ReferenceCache.lru]. Namespace is recorded so a future
// reseed can recover the exact key without trusting the filename to
// preserve it.
type referenceFileSnapshot struct {
	Namespace  string                    `json:"namespace"`
	References map[string]referenceEntry `json:"references"`
}

// Resolver is the upstream contract that [ReferenceCache.Resolve]
// consults on a cache miss. Both *remote.Repository and the
// spec.Store interface satisfy it.
type Resolver interface {
	Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error)
}

// ReferenceCache is a string-keyed LRU+TTL cache for OCI tag/digest
// reference → descriptor lookups, persisted to disk so resolves
// survive a process restart. All methods are safe for concurrent use.
//
// Entries are namespaced so two repositories that happen to share a
// short reference (e.g. the tag "v1") do not collide. On disk, each
// namespace is persisted to its own file under
// `<Options.Dir>/refs/<sha256(namespace)>.json`, mirroring the
// per-subdirectory split that [BlobCache] uses.
//
// MaxBlobSize and Accept on [Options] are ignored.
type ReferenceCache struct {
	opts   Options
	logger *slog.Logger
	lru    *expirable.LRU[referenceKey, referenceEntry]

	// mu serialises per-namespace snapshot rewrites so concurrent Add
	// calls for the same namespace do not interleave file writes.
	mu sync.Mutex

	// evictedRefsMu protects namespacesWithEvictedRefs.
	//
	// The LRU eviction callback runs synchronously during LRU
	// mutations, so it must not call writeNamespace: writeNamespace
	// reads the LRU and would deadlock on the LRU's internal lock.
	evictedRefsMu sync.Mutex

	// namespacesWithEvictedRefs contains namespaces whose persisted
	// snapshot may still contain references evicted from the in-memory
	// LRU.
	namespacesWithEvictedRefs map[string]struct{}

	// sf collapses concurrent upstream resolves for the same
	// (namespace, reference) into a single round-trip.
	sf singleflight.Group
}

// referenceKey is the in-memory LRU key. Splitting namespace and
// reference into struct fields means we don't have to choose a
// separator that escapes safely — Go's map equality covers it.
type referenceKey struct {
	namespace string
	reference string
}

// NewReferenceCache constructs a [ReferenceCache]. [Options.Dir] is
// required; other zero-valued fields fall back to [Defaults].
//
// On startup, NewReferenceCache walks `<Dir>/refs/*.json` and
// reseeds the LRU so previously resolved references survive a
// process restart. Malformed snapshots are logged and treated as
// empty.
func NewReferenceCache(opts Options) (*ReferenceCache, error) {
	opts, err := opts.applyDefaults()
	if err != nil {
		return nil, err
	}
	// Scope to a dedicated subdirectory so the same Options.Dir can
	// also host a [BlobCache] (which uses `<Dir>/blobs/...`) without
	// collisions.
	opts.Dir = filepath.Join(opts.Dir, referenceSubdir)
	if err := ensureDir(opts.Dir); err != nil {
		return nil, fmt.Errorf("refcache: %w", err)
	}

	c := &ReferenceCache{
		opts:                      opts,
		logger:                    slog.Default().With(slog.String("dir", opts.Dir)),
		namespacesWithEvictedRefs: make(map[string]struct{}),
	}
	c.lru = expirable.NewLRU(
		opts.MaxEntries,
		func(k referenceKey, _ referenceEntry) {
			c.markNamespaceWithEvictedRef(k.namespace)
			c.logger.Debug("refcache: evicted",
				slog.String("namespace", k.namespace),
				slog.String("reference", k.reference))
		},
		opts.TTL,
	)

	startRefs := time.Now()
	if loaded, err := c.load(); err != nil {
		c.logger.Warn("refcache: load snapshot failed",
			slog.String("err", err.Error()))
	} else if loaded > 0 {
		c.logger.Debug("refcache: seeded from disk",
			slog.Int("loaded", loaded),
			slog.Duration("duration", time.Since(startRefs)))
	}

	// Flush namespaces dirtied by load-time capacity eviction so stale
	// snapshot files do not survive a restart when MaxEntries is
	// smaller than the number of persisted entries.
	for namespace := range c.drainNamespacesWithEvictedRefs() {
		if err := c.writeNamespace(namespace); err != nil {
			c.logger.Warn("refcache: write snapshot after load eviction failed",
				slog.String("namespace", namespace),
				slog.String("err", err.Error()))
		}
	}

	return c, nil
}

// Add stores a (namespace, reference) → descriptor mapping in the
// in-memory LRU and persists the namespace's snapshot file so a
// future run pointing at the same Dir reseeds the mapping.
//
// When adding to a full LRU causes a capacity eviction, Add also
// rewrites the snapshot file of the evicted entry's namespace so the
// on-disk state stays consistent with memory.
//
// Add is best-effort with respect to disk: if a rewrite fails, the
// in-memory entry is still added and a warning is logged. The caller
// never sees an error from this method.
func (c *ReferenceCache) Add(ref registry.Reference, desc ociImageSpecV1.Descriptor) {
	k := c.buildKey(ref)

	c.lru.Add(k, referenceEntry{Descriptor: desc, SavedAt: time.Now()})

	// Collect namespaces dirtied by capacity eviction triggered above,
	// then always include the current namespace.
	namespaces := c.drainNamespacesWithEvictedRefs()
	namespaces[k.namespace] = struct{}{}

	for namespace := range namespaces {
		if err := c.writeNamespace(namespace); err != nil {
			c.logger.Warn("refcache: write snapshot failed",
				slog.String("namespace", namespace),
				slog.String("reference", k.reference),
				slog.String("err", err.Error()))
		}
	}
}

// Invalidate removes the (namespace, reference) mapping from the
// in-memory LRU and rewrites the namespace's on-disk snapshot so the
// stale mapping does not survive a process restart. It is a no-op if
// the entry is not cached. Callers must invalidate whenever a tag is
// re-pointed or deleted upstream, because OCI tags are mutable and the
// cache otherwise keeps serving the old descriptor until TTL expiry.
func (c *ReferenceCache) Invalidate(ref registry.Reference) {
	if c == nil {
		return
	}
	k := c.buildKey(ref)
	c.lru.Remove(k)
	if err := c.writeNamespace(k.namespace); err != nil {
		c.logger.Warn("refcache: write snapshot after invalidate failed",
			slog.String("namespace", k.namespace),
			slog.String("reference", k.reference),
			slog.String("err", err.Error()))
	}
}

// Lookup returns the cached descriptor for the (namespace, reference)
// pair and whether it was found. See [ReferenceCache.Add] for the
// namespace contract.
func (c *ReferenceCache) Lookup(ref registry.Reference) (ociImageSpecV1.Descriptor, bool) {
	v, ok := c.lru.Get(c.buildKey(ref))
	return v.Descriptor, ok
}

// Resolve consults the reference cache before delegating to upstream.
// On miss it calls upstream.Resolve and, on success, records the
// (namespace, reference) → descriptor mapping (in-memory + on disk).
// Errors are returned unchanged and not cached.
//
// See [ReferenceCache.Add] for the namespace contract. A nil receiver
// is supported: the call falls straight through to upstream so
// resolver decorators can compose without nil-checks.
func (c *ReferenceCache) Resolve(ctx context.Context, upstream Resolver, ref registry.Reference) (ociImageSpecV1.Descriptor, error) {
	if c == nil {
		return upstream.Resolve(ctx, ref.Reference)
	}
	k := c.buildKey(ref)
	if c.opts.RemotePolicy == RemotePolicyIfNotPresent {
		if v, ok := c.lru.Get(k); ok {
			c.logger.DebugContext(ctx, "refcache: hit",
				slog.String("namespace", k.namespace),
				slog.String("reference", k.reference),
				slog.String("digest", v.Descriptor.Digest.String()))
			return v.Descriptor, nil
		}
	}
	// Collapse concurrent misses (or Always-policy calls) for the same
	// key into one upstream round-trip.
	v, err, _ := c.sf.Do(k.namespace+"\x00"+ref.Reference, func() (any, error) {
		if c.opts.RemotePolicy == RemotePolicyIfNotPresent {
			if v, ok := c.lru.Get(k); ok {
				return v.Descriptor, nil
			}
		}
		desc, err := upstream.Resolve(ctx, ref.Reference)
		if err != nil {
			return ociImageSpecV1.Descriptor{}, err
		}
		c.Add(ref, desc)
		return desc, nil
	})
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	return v.(ociImageSpecV1.Descriptor), nil
}

// markNamespaceWithEvictedRef records namespace as having at least one
// reference evicted from the LRU. Safe to call from the LRU eviction
// callback because it only touches evictedRefsMu, not the LRU.
func (c *ReferenceCache) markNamespaceWithEvictedRef(namespace string) {
	c.evictedRefsMu.Lock()
	defer c.evictedRefsMu.Unlock()

	c.namespacesWithEvictedRefs[namespace] = struct{}{}
}

// drainNamespacesWithEvictedRefs atomically returns and clears the set
// of namespaces that had LRU-capacity evictions since the last drain.
func (c *ReferenceCache) drainNamespacesWithEvictedRefs() map[string]struct{} {
	c.evictedRefsMu.Lock()
	defer c.evictedRefsMu.Unlock()

	namespaces := c.namespacesWithEvictedRefs
	c.namespacesWithEvictedRefs = make(map[string]struct{})
	return namespaces
}

// buildKey constructs the in-memory cache key from the parsed registry.Reference.
func (c *ReferenceCache) buildKey(ref registry.Reference) referenceKey {
	return referenceKey{
		namespace: ref.Registry + "/" + ref.Repository,
		reference: ref.Reference,
	}
}

// pathForNamespace returns the absolute path of the snapshot file
// owned by namespace. The filename is the hex-encoded SHA-256 of the
// namespace string; see [referenceSubdir] for why a cryptographic
// digest is used purely to derive a collision-free filename.
func (c *ReferenceCache) pathForNamespace(namespace string) string {
	sum := sha256.Sum256([]byte(namespace))
	return filepath.Join(c.opts.Dir, hex.EncodeToString(sum[:])+referenceFileExt)
}

// snapshotNamespace materialises the LRU entries for a single
// namespace into a referenceFileSnapshot. It iterates Keys+Peek so it
// can run while other goroutines hold the LRU; expirable.LRU is
// internally locked. Callers must hold c.mu when writing the returned
// snapshot to disk so concurrent writers cannot persist a stale copy.
func (c *ReferenceCache) snapshotNamespace(namespace string) referenceFileSnapshot {
	out := referenceFileSnapshot{
		Namespace:  namespace,
		References: make(map[string]referenceEntry),
	}
	for _, k := range c.lru.Keys() {
		if k.namespace != namespace {
			continue
		}
		if v, ok := c.lru.Peek(k); ok {
			out.References[k.reference] = v
		}
	}
	return out
}

// writeNamespace rewrites the namespace's snapshot file atomically
// using whatever the LRU currently holds for that namespace.
// Concurrent calls are serialised via c.mu so the final rename
// always reflects a coherent snapshot of one of them.
//
// When the LRU has no entries for the namespace (e.g. all evicted),
// the snapshot file is removed so the directory does not accumulate
// empty stubs.
func (c *ReferenceCache) writeNamespace(namespace string) error {
	path := c.pathForNamespace(namespace)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Snapshot while holding c.mu so concurrent writers cannot persist
	// a stale view and clobber the newer on-disk state.
	snap := c.snapshotNamespace(namespace)
	if len(snap.References) == 0 {
		// Nothing left for this namespace; drop the stub file.
		if err := removeQuiet(path); err != nil {
			return fmt.Errorf("remove empty snapshot: %w", err)
		}
		return nil
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	return writeFileAtomic(c.opts.Dir, path, raw)
}

// load walks `<Dir>/refs/*.json` and feeds each namespace's entries
// into the LRU. Files that fail to parse are removed so the directory
// stays a clean per-namespace store; the count of dropped files is
// not surfaced because it would not be actionable.
//
// load is called once from [NewReferenceCache] before any Add can
// race with it.
func (c *ReferenceCache) load() (loaded int, err error) {
	entries, err := os.ReadDir(c.opts.Dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("read refs dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Reclaim leftover temp files from a crashed [writeFileAtomic]
		// first: their names carry a `.write-*` prefix (not a
		// `.json` extension), so the extension filter below would
		// otherwise skip them and they would accumulate indefinitely.
		if strings.HasPrefix(name, atomicWritePrefix) {
			_ = os.Remove(filepath.Join(c.opts.Dir, name))
			continue
		}
		if filepath.Ext(name) != referenceFileExt {
			continue
		}
		full := filepath.Join(c.opts.Dir, name)
		data, err := os.ReadFile(full)
		if err != nil {
			c.logger.Warn("refcache: read snapshot file",
				slog.String("file", full), slog.String("err", err.Error()))
			continue
		}
		if len(data) == 0 {
			_ = os.Remove(full)
			continue
		}
		var snap referenceFileSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			c.logger.Warn("refcache: unmarshal snapshot file",
				slog.String("file", full), slog.String("err", err.Error()))
			_ = os.Remove(full)
			continue
		}
		now := time.Now()
		for ref, entry := range snap.References {
			// Honour TTL across restarts: drop entries whose recorded
			// age already exceeds the configured TTL instead of
			// resurrecting them with a fresh in-memory expiry. SavedAt
			// is preserved so the logical age keeps advancing on the
			// next persist.
			if c.opts.TTL > 0 && !entry.SavedAt.IsZero() && now.Sub(entry.SavedAt) > c.opts.TTL {
				continue
			}
			if entry.SavedAt.IsZero() {
				entry.SavedAt = now
			}
			c.lru.Add(referenceKey{namespace: snap.Namespace, reference: ref}, entry)
			loaded++
		}
	}
	return loaded, nil
}

// atomicWritePrefix is the prefix [writeFileAtomic] uses for its
// [os.CreateTemp] pattern. The `-*` suffix stripped by CreateTemp is
// what randomises the tail; the constant is exposed so the cleanup
// scanner and the writer stay in sync.
const atomicWritePrefix = ".write-"

// writeFileAtomic writes data to path via a temp file in the same
// directory, fsyncs, and renames into place. The temp file is removed
// on any failure path.
func writeFileAtomic(dir, path string, data []byte) error {
	tmp, err := os.CreateTemp(dir, atomicWritePrefix+"*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
