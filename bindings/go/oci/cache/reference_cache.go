package cache

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// referenceSubdir is the directory under [Options.Dir] that the
// reference cache owns. Each namespace gets its own JSON file inside
// it, named after the FNV-1a hash of the namespace string. Splitting
// per namespace means an [ReferenceCache.Add] only rewrites the file
// for the affected namespace and namespaces never compete for the
// same snapshot file. The layout mirrors [BlobCache]'s
// `<Dir>/blobs/...` scoping.
//
// FNV is chosen for hashing because we only need a stable filename
// derived from the namespace — not a cryptographic property — and
// FNV's small fixed output keeps the filename compact. The canonical
// namespace string is also written into the snapshot file body, so
// any unlikely hash collision is recoverable on reseed (the colliding
// writer simply overwrites the file; the loser's entries age out via
// TTL).
const referenceSubdir = "refs"

// referenceFileExt is appended to the hashed namespace to form the
// per-namespace snapshot filename.
const referenceFileExt = ".json"

// referenceFileSnapshot is the on-disk shape of a single namespace's
// portion of [ReferenceCache.lru]. Namespace is recorded so a future
// reseed can recover the exact key without trusting the filename to
// preserve it.
type referenceFileSnapshot struct {
	Namespace  string                               `json:"namespace"`
	References map[string]ociImageSpecV1.Descriptor `json:"references"`
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
	lru    *expirable.LRU[referenceKey, ociImageSpecV1.Descriptor]

	// mu serialises per-namespace snapshot rewrites so concurrent Add
	// calls for the same namespace do not interleave file writes.
	mu sync.Mutex
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
		opts:   opts,
		logger: slog.Default().With(slog.String("dir", opts.Dir)),
	}
	c.lru = expirable.NewLRU(
		opts.MaxEntries,
		func(k referenceKey, _ ociImageSpecV1.Descriptor) {
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
	return c, nil
}

// Add stores a (namespace, reference) → descriptor mapping in the
// in-memory LRU and persists the namespace's snapshot file so a
// future run pointing at the same Dir reseeds the mapping.
//
// The namespace argument scopes the entry to a particular repository
// so two unrelated repositories that happen to use the same short
// reference do not collide. Pass an empty string when the reference
// is already globally unique; otherwise use a stable
// repository-level identifier such as "<registry>/<repository>".
//
// Add is best-effort with respect to disk: if the rewrite fails, the
// in-memory entry is still added and a warning is logged. The caller
// never sees an error from this method.
func (c *ReferenceCache) Add(namespace, reference string, desc ociImageSpecV1.Descriptor) {
	c.lru.Add(referenceKey{namespace, reference}, desc)
	if err := c.writeNamespace(namespace); err != nil {
		c.logger.Warn("refcache: write snapshot failed",
			slog.String("namespace", namespace),
			slog.String("reference", reference),
			slog.String("err", err.Error()))
	}
}

// Lookup returns the cached descriptor for the (namespace, reference)
// pair and whether it was found. See [ReferenceCache.Add] for the
// namespace contract.
func (c *ReferenceCache) Lookup(namespace, reference string) (ociImageSpecV1.Descriptor, bool) {
	return c.lru.Get(referenceKey{namespace, reference})
}

// Resolve consults the reference cache before delegating to upstream.
// On miss it calls upstream.Resolve and, on success, records the
// (namespace, reference) → descriptor mapping (in-memory + on disk).
// Errors are returned unchanged and not cached.
//
// See [ReferenceCache.Add] for the namespace contract. A nil receiver
// is supported: the call falls straight through to upstream so
// resolver decorators can compose without nil-checks.
func (c *ReferenceCache) Resolve(ctx context.Context, upstream Resolver, namespace, reference string) (ociImageSpecV1.Descriptor, error) {
	if c == nil {
		return upstream.Resolve(ctx, reference)
	}
	if desc, ok := c.lru.Get(referenceKey{namespace, reference}); ok {
		c.logger.DebugContext(ctx, "refcache: hit",
			slog.String("namespace", namespace),
			slog.String("reference", reference),
			slog.String("digest", desc.Digest.String()))
		return desc, nil
	}
	desc, err := upstream.Resolve(ctx, reference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	c.Add(namespace, reference, desc)
	return desc, nil
}

// pathForNamespace returns the absolute path of the snapshot file
// owned by namespace. The filename is the hex-encoded FNV-1a hash of
// the namespace string; see [referenceSubdir] for why a non-crypto
// hash is sufficient.
func (c *ReferenceCache) pathForNamespace(namespace string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(namespace))
	return filepath.Join(c.opts.Dir, hex.EncodeToString(h.Sum(nil))+referenceFileExt)
}

// snapshotNamespace materialises the LRU entries for a single
// namespace into a referenceFileSnapshot. It iterates Keys+Peek so it
// can run while other goroutines hold the LRU; expirable.LRU is
// internally locked.
func (c *ReferenceCache) snapshotNamespace(namespace string) referenceFileSnapshot {
	out := referenceFileSnapshot{
		Namespace:  namespace,
		References: make(map[string]ociImageSpecV1.Descriptor),
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
	snap := c.snapshotNamespace(namespace)
	path := c.pathForNamespace(namespace)

	c.mu.Lock()
	defer c.mu.Unlock()

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
		if filepath.Ext(name) != referenceFileExt {
			continue
		}
		// Skip stray temp files left from a crashed write.
		if hasIncomingPrefix(name) {
			_ = os.Remove(filepath.Join(c.opts.Dir, name))
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
		for ref, desc := range snap.References {
			c.lru.Add(referenceKey{namespace: snap.Namespace, reference: ref}, desc)
			loaded++
		}
	}
	return loaded, nil
}

// hasIncomingPrefix reports whether name is a leftover temp file
// from a crashed [writeFileAtomic] call.
func hasIncomingPrefix(name string) bool {
	const prefix = ".write-"
	return len(name) >= len(prefix) && name[:len(prefix)] == prefix
}

// writeFileAtomic writes data to path via a temp file in the same
// directory, fsyncs, and renames into place. The temp file is removed
// on any failure path.
func writeFileAtomic(dir, path string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".write-*")
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
