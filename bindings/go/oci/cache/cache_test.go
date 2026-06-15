package cache

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCache(t *testing.T, opts Options) *BlobCache {
	t.Helper()
	if opts.Dir == "" {
		opts.Dir = filepath.Join(t.TempDir(), "blobcache")
	}
	if opts.Accept == nil {
		opts.Accept = DefaultAccept
	}
	c, err := NewBlobCache(opts)
	require.NoError(t, err)
	return c
}

func TestNew_RequiresDir(t *testing.T) {
	_, err := NewBlobCache(Options{})
	require.Error(t, err)
}

func TestCache_HitMiss(t *testing.T) {
	c := newTestCache(t, Options{})

	data := []byte("manifest-bytes")
	dgst := digest.FromBytes(data)

	assert.False(t, c.Has(dgst))
	f, hit, err := c.Get(dgst)
	require.NoError(t, err)
	assert.False(t, hit)
	assert.Nil(t, f)

	inserted, err := c.Populate(t.Context(), dgst, int64(len(data)), bytes.NewReader(data))
	require.NoError(t, err)
	assert.True(t, inserted)
	assert.True(t, c.Has(dgst))

	f, hit, err = c.Get(dgst)
	require.NoError(t, err)
	require.True(t, hit)
	defer f.Close()
	got, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestCache_TTLExpiry(t *testing.T) {
	c := newTestCache(t, Options{TTL: 50 * time.Millisecond})
	data := []byte("ttl-bytes")
	dgst := digest.FromBytes(data)

	_, err := c.Populate(t.Context(), dgst, int64(len(data)), bytes.NewReader(data))
	require.NoError(t, err)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !c.Has(dgst) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.False(t, c.Has(dgst), "entry must be evicted after TTL")

	// File on disk must be gone too.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, err := os.Stat(pathFor(c.opts.Dir, dgst))
		if err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err = os.Stat(pathFor(c.opts.Dir, dgst))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestCache_LRUOverflow(t *testing.T) {
	c := newTestCache(t, Options{MaxEntries: 2})
	ctx := t.Context()

	d1 := digest.FromBytes([]byte("a"))
	d2 := digest.FromBytes([]byte("b"))
	d3 := digest.FromBytes([]byte("c"))

	_, err := c.Populate(ctx, d1, 1, bytes.NewReader([]byte("a")))
	require.NoError(t, err)
	_, err = c.Populate(ctx, d2, 1, bytes.NewReader([]byte("b")))
	require.NoError(t, err)
	_, err = c.Populate(ctx, d3, 1, bytes.NewReader([]byte("c")))
	require.NoError(t, err)

	assert.False(t, c.Has(d1), "oldest must be evicted")
	assert.True(t, c.Has(d2))
	assert.True(t, c.Has(d3))

	// File of evicted d1 must be gone on disk too.
	_, err = os.Stat(pathFor(c.opts.Dir, d1))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestCache_SizeCap(t *testing.T) {
	c := newTestCache(t, Options{MaxBlobSize: 4})
	dgst := digest.FromBytes([]byte("0123456789"))

	inserted, err := c.Populate(t.Context(), dgst, 10, bytes.NewReader([]byte("0123456789")))
	require.NoError(t, err)
	assert.False(t, inserted, "Populate must skip blobs above MaxBlobSize")
	assert.False(t, c.Has(dgst))

	_, err = os.Stat(pathFor(c.opts.Dir, dgst))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestCache_TruncatedReadDiscarded(t *testing.T) {
	c := newTestCache(t, Options{})
	dgst := digest.FromBytes([]byte("expected-bytes"))

	// Claim size=14 but feed only 4 bytes — Populate must reject.
	inserted, err := c.Populate(t.Context(), dgst, 14, bytes.NewReader([]byte("abcd")))
	require.Error(t, err)
	assert.False(t, inserted)
	assert.False(t, c.Has(dgst))
	_, err = os.Stat(pathFor(c.opts.Dir, dgst))
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

// countingReader counts bytes read so we can assert dedup-style
// invariants in store-level tests.
type countingReader struct {
	r io.Reader
	n atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

func TestCache_ConcurrentPopulate_AllSucceed(t *testing.T) {
	c := newTestCache(t, Options{})
	data := bytes.Repeat([]byte("x"), 1024)
	dgst := digest.FromBytes(data)

	// Each goroutine has its own reader (matches the io.Pipe-per-fetch
	// shape in CachingStore.Fetch). They race on os.Rename; both
	// produce identical content because the digest is the key.
	const N = 8
	var wg sync.WaitGroup
	for range N {
		wg.Go(func() {
			_, err := c.Populate(t.Context(), dgst, int64(len(data)), bytes.NewReader(data))
			require.NoError(t, err)
		})
	}
	wg.Wait()

	assert.True(t, c.Has(dgst))
	f, hit, err := c.Get(dgst)
	require.NoError(t, err)
	require.True(t, hit)
	defer f.Close()
	got, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestCache_AcceptFilter(t *testing.T) {
	cases := []struct {
		mt   string
		want bool
	}{
		{ociImageSpecV1.MediaTypeImageManifest, true},
		{ociImageSpecV1.MediaTypeImageIndex, true},
		{"application/vnd.docker.distribution.manifest.v2+json", true},
		{"application/vnd.docker.distribution.manifest.list.v2+json", true},
		{"application/vnd.ocm.software.component-descriptor.v2+json", true},
		{"application/vnd.ocm.software.component-descriptor.v2+yaml", true},
		{"application/vnd.ocm.software.component-descriptor.v1+yaml+tar", true},
		{"application/octet-stream", false},
		{ociImageSpecV1.MediaTypeImageLayerGzip, false},
	}
	for _, tc := range cases {
		t.Run(tc.mt, func(t *testing.T) {
			assert.Equal(t, tc.want, DefaultAccept(tc.mt))
		})
	}
}

func TestCache_GetFileVanished(t *testing.T) {
	c := newTestCache(t, Options{})
	data := []byte("vanish")
	dgst := digest.FromBytes(data)
	_, err := c.Populate(t.Context(), dgst, int64(len(data)), bytes.NewReader(data))
	require.NoError(t, err)

	// Externally remove the file — Get must now report a miss and
	// drop the entry.
	require.NoError(t, os.Remove(pathFor(c.opts.Dir, dgst)))

	f, hit, err := c.Get(dgst)
	require.NoError(t, err)
	assert.False(t, hit)
	assert.Nil(t, f)
	assert.False(t, c.Has(dgst))
}

func TestCache_Reseed_FromExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobcache")

	c1, err := NewBlobCache(Options{Dir: dir, Accept: DefaultAccept})
	require.NoError(t, err)
	data := []byte("persistent")
	dgst := digest.FromBytes(data)
	_, err = c1.Populate(t.Context(), dgst, int64(len(data)), bytes.NewReader(data))
	require.NoError(t, err)
	assert.True(t, c1.Has(dgst))

	// Pretend the process restarts: drop c1 entirely and build a
	// fresh BlobCache pointing at the same directory.
	c2, err := NewBlobCache(Options{Dir: dir, Accept: DefaultAccept})
	require.NoError(t, err)

	assert.True(t, c2.Has(dgst), "reseeded cache must know about prior entries")
	f, hit, err := c2.Get(dgst)
	require.NoError(t, err)
	require.True(t, hit)
	defer f.Close()
	got, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestCache_Reseed_StripsBadFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "blobcache")
	// BlobCache scopes its files under <Dir>/blobs/<algo>/...
	algo := filepath.Join(dir, "blobs", "sha256")
	require.NoError(t, os.MkdirAll(algo, 0o700))
	bad := filepath.Join(algo, "not-a-hex")
	require.NoError(t, os.WriteFile(bad, []byte("x"), 0o600))
	leftover := filepath.Join(algo, ".incoming-stale")
	require.NoError(t, os.WriteFile(leftover, []byte("y"), 0o600))

	_, err := NewBlobCache(Options{Dir: dir, Accept: DefaultAccept})
	require.NoError(t, err)

	for _, p := range []string{bad, leftover} {
		_, err := os.Stat(p)
		assert.ErrorIs(t, err, fs.ErrNotExist, "stray file %s must be removed", p)
	}
}

// errReader returns the configured error on every Read.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestCache_New_RequiresDir(t *testing.T) {
	_, err := NewBlobCache(Options{})
	require.Error(t, err)
}

func TestCache_New_EnsureDirFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	_, err := NewBlobCache(Options{Dir: filepath.Join(blocker, "child")})
	require.Error(t, err)
}

func TestCache_New_LoadReferencesMalformed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "refcache")
	refsDir := filepath.Join(dir, referenceSubdir)
	require.NoError(t, os.MkdirAll(refsDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(refsDir, "garbage"+referenceFileExt), []byte("not json"), 0o600))

	// Malformed snapshot is logged but does not abort construction.
	c, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)
	_, hit := c.Lookup("ns", "x")
	assert.False(t, hit)
}

func TestCache_New_LoadReferencesEmptyFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "refcache")
	refsDir := filepath.Join(dir, referenceSubdir)
	require.NoError(t, os.MkdirAll(refsDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(refsDir, "empty"+referenceFileExt), nil, 0o600))
	_, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)
}

func TestCache_OnEvict_LogsButDoesNotPanic(t *testing.T) {
	c := newTestCache(t, Options{})
	// Manually invoke onEvict with a path that never existed; the
	// callback must swallow the error path through removeQuiet
	// (fs.ErrNotExist is silenced) without logging a warning.
	c.onEvict(digest.FromBytes([]byte("x")), blobEntry{
		path: filepath.Join(c.opts.Dir, "missing"),
		size: 1,
	})
}

func TestCache_OnEvict_WarnPathOnRemoveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("non-empty dir remove semantics differ on windows")
	}
	c := newTestCache(t, Options{})
	// removeQuiet returns an error when path is a non-empty dir.
	dir := filepath.Join(t.TempDir(), "non-empty")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "child"), nil, 0o600))

	// Manually invoke the eviction callback with the bad path.
	c.onEvict(digest.FromBytes([]byte("y")), blobEntry{path: dir, size: 1})
}

func TestCache_Populate_OversizeDrainsAndSkips(t *testing.T) {
	c := newTestCache(t, Options{MaxBlobSize: 4})
	dgst := digest.FromBytes([]byte("0123456789"))

	cr := &countingReader{r: bytes.NewReader([]byte("0123456789"))}
	inserted, err := c.Populate(t.Context(), dgst, 10, cr)
	require.NoError(t, err)
	assert.False(t, inserted)
	assert.False(t, c.Has(dgst))
	// Populate must drain so the producer side does not block.
	assert.EqualValues(t, 10, cr.n.Load())
}

func TestCache_Populate_ReaderErrorIsReported(t *testing.T) {
	c := newTestCache(t, Options{})
	dgst := digest.FromBytes([]byte("x"))
	_, err := c.Populate(t.Context(), dgst, 4, errReader{err: errors.New("boom")})
	require.Error(t, err)
	assert.False(t, c.Has(dgst))
}

func TestCache_Get_StalePathBecomesMiss(t *testing.T) {
	c := newTestCache(t, Options{})
	dgst := digest.FromBytes([]byte("vanish"))
	_, err := c.Populate(t.Context(), dgst, 6, bytes.NewReader([]byte("vanish")))
	require.NoError(t, err)
	require.True(t, c.Has(dgst))

	// Remove the underlying file out from under the cache.
	require.NoError(t, os.Remove(pathFor(c.opts.Dir, dgst)))

	f, hit, err := c.Get(dgst)
	require.NoError(t, err)
	assert.False(t, hit)
	assert.Nil(t, f)
	// Stale entry must have been dropped from the LRU.
	assert.False(t, c.Has(dgst))
}

func TestCache_Get_NonNotExistErrorIsReturned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dir-as-file semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission bits")
	}
	c := newTestCache(t, Options{})
	dgst := digest.FromBytes([]byte("dirtrick"))
	_, err := c.Populate(t.Context(), dgst, 8, bytes.NewReader([]byte("dirtrick")))
	require.NoError(t, err)
	parent := filepath.Dir(pathFor(c.opts.Dir, dgst))
	require.NoError(t, os.Chmod(parent, 0))
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	_, hit, err := c.Get(dgst)
	require.Error(t, err)
	assert.False(t, hit)
}
