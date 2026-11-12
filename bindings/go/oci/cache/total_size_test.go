package cache

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func populateBytes(t *testing.T, c *BlobCache, data []byte) digest.Digest {
	t.Helper()
	dgst := digest.FromBytes(data)
	_, err := c.populate(t.Context(), dgst, int64(len(data)), bytes.NewReader(data))
	require.NoError(t, err)
	return dgst
}

func requireEvicted(t *testing.T, c *BlobCache, dgst digest.Digest) {
	t.Helper()
	require.False(t, c.Has(dgst), "%s must be evicted", dgst)
	_, err := os.Stat(pathFor(c.opts.Dir, dgst))
	require.ErrorIs(t, err, fs.ErrNotExist, "the file of %s must be deleted", dgst)
}

func TestCache_MaxTotalSize_EvictsLeastRecentlyUsed(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{MaxTotalSize: 10})

	d1 := populateBytes(t, c, []byte("aaaa"))
	d2 := populateBytes(t, c, []byte("bbbb"))
	f, hit, err := c.Get(d1) // d2 is now the least recently used
	r.NoError(err)
	r.True(hit)
	r.NoError(f.Close())
	d3 := populateBytes(t, c, []byte("cccc"))

	requireEvicted(t, c, d2)
	r.True(c.Has(d1))
	r.True(c.Has(d3))
	r.EqualValues(8, c.totalSize())
}

func TestCache_MaxTotalSize_ZeroMeansNoLimit(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{})
	for _, b := range []string{"aaaa", "bbbb", "cccc"} {
		r.True(c.Has(populateBytes(t, c, []byte(b))))
	}
	r.EqualValues(12, c.totalSize())
}

func TestCache_MaxTotalSize_BlobAboveLimitIsNotCached(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{MaxTotalSize: 4})
	d1 := populateBytes(t, c, []byte("aaaa"))

	data := []byte("0123456789")
	dgst := digest.FromBytes(data)
	inserted, err := c.populate(t.Context(), dgst, int64(len(data)), bytes.NewReader(data))
	r.NoError(err)
	r.False(inserted)
	requireEvicted(t, c, dgst)
	r.True(c.Has(d1), "a blob that can never fit must not evict others")
	r.EqualValues(4, c.totalSize())
}

func TestCache_MaxTotalSize_RepopulateCountsOnce(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{MaxTotalSize: 10})
	d1 := populateBytes(t, c, []byte("aaaa"))
	populateBytes(t, c, []byte("aaaa"))
	r.EqualValues(4, c.totalSize())

	d2 := populateBytes(t, c, []byte("bbbb"))
	r.True(c.Has(d1))
	r.True(c.Has(d2))
	r.EqualValues(8, c.totalSize())
}

func TestCache_MaxTotalSize_ReseedTrimsOldestFirst(t *testing.T) {
	r := require.New(t)
	dir := filepath.Join(t.TempDir(), "blobcache")
	c1 := newTestCache(t, Options{Dir: dir})
	d1 := populateBytes(t, c1, []byte("aaaa"))
	d2 := populateBytes(t, c1, []byte("bbbb"))
	d3 := populateBytes(t, c1, []byte("cccc"))
	// Directory order says nothing about age; only modification times do.
	now := time.Now()
	for i, d := range []digest.Digest{d2, d3, d1} {
		mtime := now.Add(time.Duration(i-3) * time.Minute)
		r.NoError(os.Chtimes(pathFor(c1.opts.Dir, d), mtime, mtime))
	}

	c2 := newTestCache(t, Options{Dir: dir, MaxTotalSize: 8})

	requireEvicted(t, c2, d2)
	r.True(c2.Has(d3))
	r.True(c2.Has(d1))
	r.EqualValues(8, c2.totalSize())
}

func TestCache_MaxTotalSize_ReseedCountsEntries(t *testing.T) {
	r := require.New(t)
	dir := filepath.Join(t.TempDir(), "blobcache")
	c1 := newTestCache(t, Options{Dir: dir})
	populateBytes(t, c1, []byte("aaaa"))
	populateBytes(t, c1, []byte("bbbb"))

	c2 := newTestCache(t, Options{Dir: dir, MaxTotalSize: 10})
	r.EqualValues(8, c2.totalSize())
	d3 := populateBytes(t, c2, []byte("cccc"))
	r.True(c2.Has(d3))
	r.EqualValues(8, c2.totalSize(), "reseeded entries count against the limit")
}

func TestCache_MaxTotalSize_TTLExpiryLowersTotal(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{TTL: 50 * time.Millisecond, MaxTotalSize: 10})
	populateBytes(t, c, []byte("aaaa"))
	r.EqualValues(4, c.totalSize())

	r.Eventually(func() bool { return c.totalSize() == 0 }, 2*time.Second, 10*time.Millisecond)
}

func TestCache_MaxTotalSize_ConcurrentPopulateCountsOnce(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{MaxTotalSize: 1 << 20})
	data := []byte("concurrent")
	dgst := digest.FromBytes(data)

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			_, err := c.populate(t.Context(), dgst, int64(len(data)), bytes.NewReader(data))
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	r.True(c.Has(dgst))
	r.EqualValues(len(data), c.totalSize())
}

func TestCache_Fetch_BlobAboveMaxTotalSizeFallsThrough(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{Accept: layerAccept(64), MaxTotalSize: 4})
	base := newFakeReadOnly()
	desc := base.put(ociImageSpecV1.MediaTypeImageLayer, []byte("0123456789"))

	fetchTwice(t, c, base, desc)

	r.EqualValues(2, base.fetches.Load())
	r.False(c.Has(desc.Digest))
}

// layerAccept admits what [DefaultAccept] admits plus blobs of known size up to limit.
func layerAccept(limit int64) func(ociImageSpecV1.Descriptor) bool {
	return func(d ociImageSpecV1.Descriptor) bool {
		return DefaultAccept(d) || (d.Size > 0 && d.Size <= limit)
	}
}

func fetchTwice(t *testing.T, c *BlobCache, base ReadOnlyRepository, desc ociImageSpecV1.Descriptor) {
	t.Helper()
	for range 2 {
		rc, err := c.Fetch(t.Context(), base, desc)
		require.NoError(t, err)
		_ = readAllAndClose(t, rc)
	}
}
