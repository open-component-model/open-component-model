package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// blobEntry is the LRU value for [BlobCache]. The on-disk path is
// derived deterministically from the digest key via [pathFor]; storing
// it on the entry keeps the eviction callback self-contained even if
// the layout function changes.
type blobEntry struct {
	path string
	size int64
}

// BlobCache is a digest-keyed, disk-backed LRU+TTL cache for small
// immutable blobs (manifests and OCM component descriptors). All
// methods are safe for concurrent use.
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
}

// NewBlobCache constructs a [BlobCache]. [Options.Dir] is required;
// other zero-valued fields fall back to [Defaults].
//
// The cache stores its files under `<Options.Dir>/blobs/<algo>/<hex>`
// so the same Dir can also host a [ReferenceCache] (which writes
// `<Dir>/refs/<fnv1a(namespace)>.json`) without collisions.
func NewBlobCache(opts Options) (*BlobCache, error) {
	opts, err := opts.applyDefaults()
	if err != nil {
		return nil, err
	}
	// Scope to a dedicated subdirectory so a shared Options.Dir does
	// not entangle blob layout with anything else the caller stores
	// at that path.
	opts.Dir = filepath.Join(opts.Dir, "blobs")
	if err := ensureDir(opts.Dir); err != nil {
		return nil, fmt.Errorf("blobcache: %w", err)
	}

	c := &BlobCache{opts: opts, logger: slog.Default().With(slog.String("dir", opts.Dir))}
	c.lru = expirable.NewLRU(opts.MaxEntries, c.onEvict, opts.TTL)

	startScan := time.Now()
	existing, err := scanExisting(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("blobcache: scan existing entries: %w", err)
	}
	totalSize := int64(0)
	for _, e := range existing {
		c.lru.Add(e.digest, blobEntry{path: e.path, size: e.size})
		totalSize += e.size
	}
	if len(existing) > 0 {
		c.logger.Debug("blobcache: seeded from disk",
			slog.Int("count", len(existing)),
			slog.Duration("duration", time.Since(startScan)),
			slog.Int64("size", totalSize))
	}
	return c, nil
}

// Accept reports whether mediaType should be cached, per [Options.Accept].
func (c *BlobCache) Accept(mediaType string) bool {
	return c.opts.Accept(mediaType)
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

// Populate writes r to disk under dgst and inserts the entry into the
// LRU. It is a no-op (returns false, nil) when:
//
//   - [Options.MaxBlobSize] > 0 and size exceeds it.
//   - The blob copied does not match size (when size > 0). This guards
//     against truncated reads from early-Close on the upstream reader.
//
// Concurrent calls for the same digest race on the final [os.Rename];
// the last winner wins. Both yield byte-identical content (the digest
// is the key) so the LRU bookkeeping is harmless. Callers that want
// to avoid duplicate upstream fetches must dedupe at a higher layer.
//
// The boolean return value reports whether the blob was actually
// inserted into the LRU (true) or skipped (false).
func (c *BlobCache) Populate(ctx context.Context, dgst digest.Digest, size int64, r io.Reader) (bool, error) {
	if c.opts.MaxBlobSize > 0 && size > c.opts.MaxBlobSize {
		c.logger.DebugContext(ctx, "blobcache: skipping oversized blob",
			slog.String("digest", dgst.String()),
			slog.Int64("size", size),
			slog.Int64("max", c.opts.MaxBlobSize))
		// Drain r so the upstream pipe writer doesn't block forever
		// waiting for someone to read it.
		_, _ = io.Copy(io.Discard, r)
		return false, nil
	}

	path, n, err := writeAtomic(c.opts.Dir, dgst, c.opts.MaxBlobSize, r)
	if err != nil {
		return false, err
	}
	if size > 0 && n != size {
		if rerr := removeQuiet(path); rerr != nil {
			c.logger.WarnContext(ctx, "blobcache: remove truncated entry failed",
				slog.String("digest", dgst.String()), slog.String("err", rerr.Error()))
		}
		return false, fmt.Errorf("blobcache: short read: got %d, want %d", n, size)
	}
	c.lru.Add(dgst, blobEntry{path: path, size: n})
	c.logger.DebugContext(ctx, "blobcache: populated",
		slog.String("digest", dgst.String()), slog.Int64("size", n))
	return true, nil
}

// Fetch is the cache-aware Fetch primitive used by store
// implementations that want to layer the cache in front of an
// upstream [content.ReadOnlyStorage]. It mirrors the oras-go
// internal/cas/proxy.go pattern: cache hit returns the on-disk file;
// miss reads from upstream and tees the bytes into the cache.
//
// Fetch honours [Options.Accept] and [Options.MaxBlobSize]: if the
// descriptor fails either filter (or c is nil), the call falls
// straight through to upstream.Fetch.
//
// Concurrent calls for the same digest each issue their own
// upstream.Fetch; the [os.Rename]s race but produce byte-identical
// files because the digest is the key.
func (c *BlobCache) Fetch(ctx context.Context, upstream content.ReadOnlyStorage, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	if c == nil || !c.Accept(target.MediaType) ||
		(c.MaxBlobSize() > 0 && target.Size > c.MaxBlobSize()) {
		return upstream.Fetch(ctx, target)
	}

	if f, hit, err := c.Get(target.Digest); err != nil {
		c.logger.DebugContext(ctx, "blobcache: get failed, falling through",
			slog.String("digest", target.Digest.String()), slog.String("err", err.Error()))
	} else if hit {
		c.logger.DebugContext(ctx, "blobcache: hit",
			slog.String("digest", target.Digest.String()), slog.Int64("size", target.Size))
		return f, nil
	}

	upstreamRC, err := upstream.Fetch(ctx, target)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	var wg sync.WaitGroup
	wg.Go(func() {
		// Populate drains pr; if it errors we propagate the error
		// back through the pipe so the consumer's TeeReader Close
		// observes it.
		if _, perr := c.Populate(ctx, target.Digest, target.Size, pr); perr != nil {
			_ = pr.CloseWithError(perr)
		}
	})

	return &teeReadCloser{
		r:        io.TeeReader(upstreamRC, pw),
		upstream: upstreamRC,
		pw:       pw,
		wg:       &wg,
	}, nil
}

// onEvict is the LRU's eviction callback. It removes the on-disk file
// for the evicted entry. Errors are logged at WARN — eviction must
// not block.
func (c *BlobCache) onEvict(dgst digest.Digest, e blobEntry) {
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

// teeReadCloser is the ReadCloser returned by [BlobCache.Fetch] on a
// cache miss. It tees upstream bytes into a pipe that the background
// populate goroutine drains. Close shuts down both ends in the right
// order so the cache write completes before Close returns.
type teeReadCloser struct {
	r        io.Reader
	upstream io.ReadCloser
	pw       *io.PipeWriter
	wg       *sync.WaitGroup
}

func (t *teeReadCloser) Read(p []byte) (int, error) {
	return t.r.Read(p)
}

func (t *teeReadCloser) Close() error {
	upErr := t.upstream.Close()
	// Closing the pipe writer signals EOF to the populate goroutine so
	// writeAtomic can finalize. If the consumer closes early, Populate
	// will observe a short read and skip the LRU insert.
	_ = t.pw.Close()
	t.wg.Wait()
	return upErr
}
