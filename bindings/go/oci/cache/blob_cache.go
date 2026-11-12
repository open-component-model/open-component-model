package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/registry/remote/properties"
	"golang.org/x/sync/singleflight"
)

const (
	// blobSubdir is the directory under [Options.Dir] that holds the blobs
	// of a [BlobCache].
	blobSubdir = "blobs"
	// servedSubdir is the directory under [Options.Dir] that holds the records
	// of which repository served which blob under [RemotePolicyAlways].
	servedSubdir = "served"
)

// blobEntry is the LRU value for [BlobCache]. The on-disk path is
// derived deterministically from the digest key via [pathFor]; storing
// it on the entry keeps the eviction callback self-contained even if
// the layout function changes.
type blobEntry struct {
	path string
	size int64
}

// ReadOnlyRepository is the upstream of a [BlobCache]: read-only storage of one
// repository. ParseReference identifies that repository, so the identity under
// which [RemotePolicyAlways] records a blob comes from the same object that
// answers Exists. A [*remote.Repository] satisfies it.
type ReadOnlyRepository interface {
	content.ReadOnlyStorage
	ParseReference(reference string) (properties.Reference, error)
}

// BlobCache is a digest-keyed, disk-backed LRU+TTL cache for immutable
// blobs. By default it admits manifests, indexes and OCM component
// descriptors; a caller can admit more, such as small layers, through
// [Options.Accept], and bound the disk use with [Options.MaxTotalSize].
// All methods are safe for concurrent use.
//
// On startup, [NewBlobCache] reseeds the LRU from any well-formed
// digest-keyed files already present under [Options.Dir], so cached
// entries survive a process restart with the same Dir. Stray files
// (unparseable names, leftover .incoming-* temp files) are removed
// during the seed pass.
//
// See package documentation for the design rationale and the link to
// oras-go's proxy.go pattern that this cache is built to support.
type BlobCache struct {
	opts   Options
	logger *slog.Logger
	lru    *expirable.LRU[digest.Digest, blobEntry]

	// insertMu serializes inserts into lru, so an insert knows whether
	// its digest is already present and total counts each entry once.
	// onEvict runs under the lock of lru and must not take it.
	insertMu sync.Mutex
	// total is the sum of the sizes of the entries in lru.
	total atomic.Int64

	// served records which repository served which blob, as the qualified
	// reference of the blob. It is only set under [RemotePolicyAlways] and bounds
	// what a hit may be served to, see [BlobCache.fetchAlways]. It must not be
	// shared with a caller's [ReferenceCache]: that one records what a registry
	// resolved, which proves nothing about holding the bytes.
	served *ReferenceCache

	// sf collapses concurrent upstream fetches for the same digest into
	// a single round-trip. The shared result is buffered in memory, so
	// [Options.MaxBlobSize] and [Options.Accept] must keep admitted
	// blobs small, and each waiter is served its own reader over those
	// bytes.
	sf singleflight.Group
}

// NewBlobCache constructs a [BlobCache]. [Options.Dir] is required;
// other zero-valued fields fall back to [Defaults].
//
// The cache stores its files under `<Options.Dir>/blobs/<algo>/<hex>`
// so the same Dir can also host a [ReferenceCache] (which writes
// `<Dir>/refs/<sha256(namespace)>.json`) without collisions. Under
// [RemotePolicyAlways] it also records what each repository served, in
// a [ReferenceCache] of its own under `<Options.Dir>/served/`.
func NewBlobCache(opts Options) (*BlobCache, error) {
	opts, err := opts.applyDefaults(DefaultBlobMaxEntries)
	if err != nil {
		return nil, err
	}
	root := opts.Dir
	// Scope to a dedicated subdirectory so a shared Options.Dir does
	// not entangle blob layout with anything else the caller stores
	// at that path.
	opts.Dir = filepath.Join(root, blobSubdir)
	if err := ensureDir(opts.Dir); err != nil {
		return nil, fmt.Errorf("blobcache: %w", err)
	}

	c := &BlobCache{opts: opts, logger: slog.Default().With(slog.String("dir", opts.Dir))}
	c.lru = expirable.NewLRU(opts.MaxEntries, c.onEvict, opts.TTL)
	if opts.RemotePolicy == RemotePolicyAlways {
		c.served, err = NewReferenceCache(Options{
			Dir:        filepath.Join(root, servedSubdir),
			MaxEntries: opts.MaxEntries,
			TTL:        noExpiry, // a hit also needs the blob, so the blob's lifetime decides
		})
		if err != nil {
			return nil, fmt.Errorf("blobcache: %w", err)
		}
	}

	startScan := time.Now()
	existing, err := scanExisting(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("blobcache: scan existing entries: %w", err)
	}
	// Oldest first, so the LRU and the trim below evict the least
	// recently written entries.
	for _, e := range existing {
		c.insert(e.digest, blobEntry{path: e.path, size: e.size})
	}
	c.trim()
	if len(existing) > 0 {
		c.logger.Debug("blobcache: seeded from disk",
			slog.Int("count", len(existing)),
			slog.Int("kept", c.lru.Len()),
			slog.Duration("duration", time.Since(startScan)),
			slog.Int64("size", c.totalSize()))
	}
	return c, nil
}

// Accept reports whether desc should be cached, per [Options.Accept].
// The full descriptor is fed to the admission filter (not just the
// media type) so future extensions can key on annotations, size, or
// artifact type without a breaking signature change.
func (c *BlobCache) Accept(desc ociImageSpecV1.Descriptor) bool {
	return c.opts.Accept(desc)
}

// MaxBlobSize returns the configured per-blob size cap (0 means no cap).
func (c *BlobCache) MaxBlobSize() int64 {
	return c.opts.MaxBlobSize
}

// Has reports whether dgst is currently cached without affecting LRU
// recency.
func (c *BlobCache) Has(dgst digest.Digest) bool {
	_, ok := c.lru.Peek(dgst)
	return ok
}

// Get returns an open file positioned at offset 0 if dgst is cached.
// The caller is responsible for closing the returned file. A miss
// returns (nil, false, nil).
//
// If the LRU has the entry but the on-disk file is missing (e.g. the
// user removed the cache directory), the entry is evicted and the
// call returns a miss.
func (c *BlobCache) Get(dgst digest.Digest) (*os.File, bool, error) {
	e, ok := c.lru.Get(dgst)
	if !ok {
		return nil, false, nil
	}
	f, err := os.Open(e.path)
	if err != nil {
		c.lru.Remove(dgst)
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("blobcache: open cached file: %w", err)
	}
	return f, true, nil
}

// populate writes r to disk under dgst and inserts the entry into the
// LRU. It is a no-op (returns false, nil) when:
//
//   - [Options.MaxBlobSize] > 0 and size exceeds it.
//   - [Options.MaxTotalSize] > 0 and the blob exceeds it.
//   - The blob copied does not match size (when size > 0). This guards
//     against truncated reads from early-Close on the upstream reader.
//
// Concurrent calls for the same digest are serialized when the blob is
// moved into place, so each entry is counted once against
// [Options.MaxTotalSize]. Callers that want to avoid duplicate upstream
// fetches must dedupe at a higher layer (see [BlobCache.Fetch], which
// uses singleflight for exactly this).
//
// The boolean return value reports whether the blob was actually
// inserted into the LRU (true) or skipped (false).
func (c *BlobCache) populate(ctx context.Context, dgst digest.Digest, size int64, r io.Reader) (bool, error) {
	if c.tooLarge(size) {
		c.logger.DebugContext(ctx, "blobcache: skipping oversized blob",
			slog.String("digest", dgst.String()),
			slog.Int64("size", size),
			slog.Int64("max", c.opts.MaxBlobSize))
		// Drain r so the upstream pipe writer doesn't block forever
		// waiting for someone to read it.
		_, _ = io.Copy(io.Discard, r)
		return false, nil
	}

	tmp, path, n, err := writeTemp(c.opts.Dir, dgst, c.opts.MaxBlobSize, r)
	if err != nil {
		return false, err
	}
	if (size > 0 && n != size) || c.tooLarge(n) {
		if rerr := removeQuiet(tmp); rerr != nil {
			c.logger.WarnContext(ctx, "blobcache: remove rejected entry failed",
				slog.String("digest", dgst.String()), slog.String("err", rerr.Error()))
		}
		if c.tooLarge(n) {
			return false, nil
		}
		return false, fmt.Errorf("blobcache: short read: got %d, want %d", n, size)
	}
	if err := c.commit(dgst, tmp, blobEntry{path: path, size: n}); err != nil {
		return false, err
	}
	c.trim()
	c.logger.DebugContext(ctx, "blobcache: populated",
		slog.String("digest", dgst.String()), slog.Int64("size", n))
	return true, nil
}

// tooLarge reports whether a blob of size can never be cached, per
// [Options.MaxBlobSize] and [Options.MaxTotalSize].
func (c *BlobCache) tooLarge(size int64) bool {
	return (c.opts.MaxBlobSize > 0 && size > c.opts.MaxBlobSize) ||
		(c.opts.MaxTotalSize > 0 && size > c.opts.MaxTotalSize)
}

// commit moves the temp file tmp into place as e and inserts it. An
// entry already cached under dgst is evicted first, so its size is
// subtracted before the new one is added.
func (c *BlobCache) commit(dgst digest.Digest, tmp string, e blobEntry) error {
	c.insertMu.Lock()
	defer c.insertMu.Unlock()
	// Remove runs onEvict, which deletes the old file before the rename.
	c.lru.Remove(dgst)
	if err := commitTemp(tmp, e.path, e.size); err != nil {
		return err
	}
	c.insert(dgst, e)
	return nil
}

// insert adds e to the LRU and its size to the total. dgst must not be
// in the LRU: insert is called under insertMu after a Remove, or while
// seeding, where every digest is read from disk once.
func (c *BlobCache) insert(dgst digest.Digest, e blobEntry) {
	// Added before the entry, so an eviction of it never takes the
	// total below zero.
	c.total.Add(e.size)
	c.lru.Add(dgst, e)
}

// trim evicts least recently used entries until the cache fits
// [Options.MaxTotalSize].
func (c *BlobCache) trim() {
	if c.opts.MaxTotalSize <= 0 {
		return
	}
	for c.total.Load() > c.opts.MaxTotalSize {
		if _, _, ok := c.lru.RemoveOldest(); !ok {
			return
		}
	}
}

// totalSize returns the bytes of all cached entries.
func (c *BlobCache) totalSize() int64 {
	return c.total.Load()
}

// Fetch is the cache-aware Fetch primitive used by store
// implementations that want to layer the cache in front of an
// upstream [content.ReadOnlyStorage]. A cache hit returns the on-disk
// file directly; a miss reads the (small) blob from upstream
// verifying its digest, records it to disk, and serves the caller
// from the in-memory copy.
//
// Fetch honors [Options.Accept], [Options.MaxBlobSize] and
// [Options.MaxTotalSize]: if the descriptor fails any filter (or c is nil), the call falls
// straight through to upstream.Fetch and nothing is buffered. In the
// oversize case a debug log is emitted so operators can see why the
// cache did not engage.
//
// Concurrent Fetches for the same digest are collapsed via
// singleflight into a single upstream.Fetch; every caller receives an
// independent reader over identical bytes. Under [RemotePolicyAlways]
// every caller proves access with its own upstream: a cache hit requires
// that its repository served the blob before and that Exists passes, and
// any other call is not collapsed but fetched by the caller itself.
func (c *BlobCache) Fetch(ctx context.Context, upstream ReadOnlyRepository, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	if c == nil || !c.Accept(target) {
		return upstream.Fetch(ctx, target)
	}
	if c.tooLarge(target.Size) {
		c.logger.DebugContext(ctx, "blobcache: skipping oversized descriptor, falling through",
			slog.String("digest", target.Digest.String()),
			slog.Int64("size", target.Size),
			slog.Int64("max", c.MaxBlobSize()),
			slog.Int64("maxTotal", c.opts.MaxTotalSize))
		return upstream.Fetch(ctx, target)
	}

	if c.opts.RemotePolicy == RemotePolicyAlways {
		return c.fetchAlways(ctx, upstream, target)
	}

	if f := c.cached(ctx, target); f != nil {
		return f, nil
	}

	// Miss: collapse concurrent fetches for the same digest into one
	// upstream round-trip. The leader fetches the blob fully via
	// [content.FetchAll], which streams through a VerifyReader so the
	// bytes are hashed against target.Digest before we ever put them
	// on disk. The verified bytes are then persisted (best-effort) and
	// every waiter is served an independent reader over that copy.
	//
	// We use DoChan instead of Do so that each caller can return on its
	// own context cancellation while the shared fetch continues for any
	// remaining waiters. The fetch itself runs under context.Background
	// so it is not tied to any single caller's lifetime.
	//
	// Trade-off: if every caller cancels before the fetch completes the
	// upstream round-trip continues to completion with no one to receive
	// the result. For this cache (small manifests, bounded by
	// MaxBlobSize, registry upstreams with their own timeouts) that is
	// acceptable. A ref-counted shared context that cancels when the
	// last waiter leaves would be the more principled alternative.
	fetchCtx := context.Background()
	ch := c.sf.DoChan(target.Digest.String(), func() (any, error) {
		// singleflight only collapses calls that overlap in time: a caller
		// whose lookup above missed just before the previous flight
		// populated the cache, but whose DoChan arrives after that flight
		// returned, would otherwise start a second upstream fetch.
		if f := c.cached(fetchCtx, target); f != nil {
			data, rerr := io.ReadAll(f)
			_ = f.Close()
			if rerr == nil {
				return data, nil
			}
			c.logger.DebugContext(ctx, "blobcache: read cached file failed, falling through",
				slog.String("digest", target.Digest.String()), slog.String("err", rerr.Error()))
		}
		c.logger.Debug("blobcache: singleflight leader: starting upstream fetch",
			slog.String("digest", target.Digest.String()))
		return c.fetchAndPopulate(fetchCtx, upstream, target)
	})
	select {
	case <-ctx.Done():
		c.logger.DebugContext(ctx, "blobcache: caller canceled while waiting for fetch",
			slog.String("digest", target.Digest.String()),
			slog.String("err", ctx.Err().Error()))
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		if res.Shared {
			c.logger.DebugContext(ctx, "blobcache: singleflight: result shared across concurrent callers",
				slog.String("digest", target.Digest.String()))
		}
		return io.NopCloser(bytes.NewReader(res.Val.([]byte))), nil
	}
}

// fetchAlways serves target under [RemotePolicyAlways]. A hit is only served if
// the repository of upstream served this blob before and still allows access to
// it. Exists alone would not do: upstream is chosen by the caller, and a registry
// the caller controls can claim to hold any digest. Any other caller fetches the
// blob itself, not collapsed with others, so it only ever gets bytes its own
// repository served and that match the digest.
func (c *BlobCache) fetchAlways(ctx context.Context, upstream ReadOnlyRepository, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	blobRef, err := upstream.ParseReference(target.Digest.String())
	if err != nil {
		return nil, fmt.Errorf("blobcache: identify repository of %s: %w", target.Digest, err)
	}
	// The record must be for this digest, whatever the implementation returns.
	blobRef.Tag = ""
	blobRef.Digest = target.Digest.String()

	_, known := c.served.Lookup(blobRef)
	if known {
		if f := c.cached(ctx, target); f != nil {
			if err := c.verifyRemoteAccess(ctx, upstream, target); err != nil {
				return nil, errors.Join(err, f.Close())
			}
			return f, nil
		}
	}

	data, err := c.fetchAndPopulate(ctx, upstream, target)
	if err != nil {
		return nil, err
	}
	if !known {
		// Only the digest: annotations of a descriptor can be of any size, and
		// writing a record rewrites the snapshot of its repository.
		c.served.Add(blobRef, ociImageSpecV1.Descriptor{Digest: target.Digest})
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// fetchAndPopulate fetches target from upstream through [content.FetchAll],
// which verifies the bytes against target.Digest, and caches them.
func (c *BlobCache) fetchAndPopulate(ctx context.Context, upstream content.ReadOnlyStorage, target ociImageSpecV1.Descriptor) ([]byte, error) {
	data, err := content.FetchAll(ctx, upstream, target)
	if err != nil {
		return nil, fmt.Errorf("blobcache: fetch upstream: %w", err)
	}
	// populate is best-effort: a failure to persist must not fail
	// the fetch, since we already hold the verified bytes to serve
	// the caller.
	if _, perr := c.populate(ctx, target.Digest, target.Size, bytes.NewReader(data)); perr != nil {
		c.logger.DebugContext(ctx, "blobcache: populate failed, serving from memory",
			slog.String("digest", target.Digest.String()), slog.String("err", perr.Error()))
	}
	return data, nil
}

// cached returns an open file for target if it is in the cache, or nil
// on a miss. A failing local lookup is logged and reported as a miss so
// the caller falls through to upstream. The caller verifies remote access.
func (c *BlobCache) cached(ctx context.Context, target ociImageSpecV1.Descriptor) *os.File {
	f, hit, err := c.Get(target.Digest)
	if err != nil {
		c.logger.DebugContext(ctx, "blobcache: get failed, falling through",
			slog.String("digest", target.Digest.String()), slog.String("err", err.Error()))
		return nil
	}
	if !hit {
		return nil
	}
	c.logger.DebugContext(ctx, "blobcache: hit",
		slog.String("digest", target.Digest.String()), slog.Int64("size", target.Size))
	return f
}

// verifyRemoteAccess checks whether upstream can access target.
func (c *BlobCache) verifyRemoteAccess(ctx context.Context, upstream content.ReadOnlyStorage, target ociImageSpecV1.Descriptor) error {
	ok, err := upstream.Exists(ctx, target)
	if err != nil {
		return fmt.Errorf("blobcache: verify remote access: %w", err)
	}
	if !ok {
		return fmt.Errorf("blobcache: remote does not contain or allow access to %s", target.Digest)
	}
	return nil
}

// onEvict is the LRU's eviction callback. It removes the on-disk file
// for the evicted entry. Errors are logged at WARN — eviction must
// not block.
func (c *BlobCache) onEvict(dgst digest.Digest, e blobEntry) {
	c.total.Add(-e.size)
	if err := removeQuiet(e.path); err != nil {
		c.logger.Warn("blobcache: evict file remove failed",
			slog.String("digest", dgst.String()),
			slog.String("path", e.path),
			slog.String("err", err.Error()))
		return
	}
	c.logger.Debug("blobcache: evicted",
		slog.String("digest", dgst.String()),
		slog.Int64("size", e.size))
}
