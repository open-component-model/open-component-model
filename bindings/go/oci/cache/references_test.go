package cache

import (
	"context"
	"encoding/json"
	"errors"
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
	"oras.land/oras-go/v2/registry"
)

// resolverFn adapts a function to the upstream resolver interface
// used by [ReferenceCache.Resolve].
type resolverFn func(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error)

func (f resolverFn) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	return f(ctx, reference)
}

func newTestRefCache(t *testing.T, opts Options) *ReferenceCache {
	t.Helper()
	if opts.Dir == "" {
		opts.Dir = filepath.Join(t.TempDir(), "refcache")
	}
	if opts.RemotePolicy == "" {
		opts.RemotePolicy = RemotePolicyIfNotPresent
	}
	c, err := NewReferenceCache(opts)
	require.NoError(t, err)
	return c
}

func TestReferenceCache_Resolve_HitAfterFirstCall(t *testing.T) {
	c := newTestRefCache(t, Options{})

	manifest := []byte("manifest")
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifest),
		Size:      int64(len(manifest)),
	}

	var calls atomic.Int64
	upstream := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		calls.Add(1)
		return desc, nil
	})

	ref, err := registry.ParseReference("ghcr.io/owner/repo@" + desc.Digest.String())
	require.NoError(t, err)

	got, err := c.Resolve(t.Context(), upstream, ref)
	require.NoError(t, err)
	assert.Equal(t, desc, got)

	got2, err := c.Resolve(t.Context(), upstream, ref)
	require.NoError(t, err)
	assert.Equal(t, desc, got2)

	assert.EqualValues(t, 1, calls.Load(), "second Resolve must hit reference cache")
}

// TestReferenceCache_Resolve_TagResolvesUpstreamEveryTime pins the
// mutability contract: a tag can be re-pointed upstream at any moment,
// so every Resolve of a tag must ask the registry what it currently
// points at, and the answer must be the current one.
func TestReferenceCache_Resolve_TagResolvesUpstreamEveryTime(t *testing.T) {
	c := newTestRefCache(t, Options{})

	first := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("first")),
		Size:      5,
	}
	second := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("second")),
		Size:      6,
	}

	var calls atomic.Int64
	upstream := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		if calls.Add(1) == 1 {
			return first, nil
		}
		return second, nil
	})

	ref, err := registry.ParseReference("ghcr.io/owner/repo:v1")
	require.NoError(t, err)

	got, err := c.Resolve(t.Context(), upstream, ref)
	require.NoError(t, err)
	assert.Equal(t, first, got)

	// The tag now points somewhere else upstream.
	got2, err := c.Resolve(t.Context(), upstream, ref)
	require.NoError(t, err)
	assert.Equal(t, second, got2, "a moved tag must not be served from cache")
	assert.EqualValues(t, 2, calls.Load())
}

func TestReferenceCache_Invalidate_RemovesEntryAndSurvivesRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "refcache")
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("x")),
		Size:      1,
	}

	c1, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)

	ref, err := registry.ParseReference("ghcr.io/ns:v1")
	require.NoError(t, err)

	c1.Add(ref, desc)
	_, ok := c1.Lookup(ref)
	require.True(t, ok)

	c1.Invalidate(ref)
	_, ok = c1.Lookup(ref)
	assert.False(t, ok, "entry must be gone from the in-memory LRU")

	// A restart must not resurrect the invalidated mapping.
	c2, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)
	_, ok = c2.Lookup(ref)
	assert.False(t, ok, "invalidated mapping must not survive a restart")
}

func TestReferenceCache_Invalidate_UnknownEntryIsNoOp(t *testing.T) {
	c := newTestRefCache(t, Options{})
	ref, err := registry.ParseReference("ghcr.io/ns:never-added")
	require.NoError(t, err)
	c.Invalidate(ref)
}

func TestReferenceCache_Invalidate_NilReceiver(t *testing.T) {
	var c *ReferenceCache
	ref, err := registry.ParseReference("ghcr.io/ns:ref")
	require.NoError(t, err)
	c.Invalidate(ref)
}

func TestReferenceCache_Load_DropsExpiredEntries(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "refcache")
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("stale")),
		Size:      5,
	}

	c1, err := NewReferenceCache(Options{Dir: dir, TTL: time.Hour})
	require.NoError(t, err)

	ref, err := registry.ParseReference("ghcr.io/ns:v1")
	require.NoError(t, err)

	c1.Add(ref, desc)

	// Backdate the persisted entry beyond the TTL by rewriting its
	// snapshot file with an old savedAt.
	snapPath := c1.pathForNamespace("ghcr.io/ns")
	raw, err := os.ReadFile(snapPath)
	require.NoError(t, err)
	var snap referenceFileSnapshot
	require.NoError(t, json.Unmarshal(raw, &snap))
	entry := snap.References["v1"]
	entry.SavedAt = time.Now().Add(-2 * time.Hour)
	snap.References["v1"] = entry
	rewritten, err := json.Marshal(snap)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(snapPath, rewritten, 0o600))

	// A restart must honour the TTL and drop the stale mapping.
	c2, err := NewReferenceCache(Options{Dir: dir, TTL: time.Hour})
	require.NoError(t, err)
	_, ok := c2.Lookup(ref)
	assert.False(t, ok, "entry older than TTL must be dropped on reload")
}

func TestReferenceCache_Load_KeepsFreshEntriesWithinTTL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "refcache")
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("fresh")),
		Size:      5,
	}

	c1, err := NewReferenceCache(Options{Dir: dir, TTL: time.Hour})
	require.NoError(t, err)

	ref, err := registry.ParseReference("ghcr.io/ns:v1")
	require.NoError(t, err)

	c1.Add(ref, desc)

	c2, err := NewReferenceCache(Options{Dir: dir, TTL: time.Hour})
	require.NoError(t, err)
	got, ok := c2.Lookup(ref)
	require.True(t, ok, "a fresh entry within TTL must survive a restart")
	assert.Equal(t, desc, got)
}

func TestReferenceCache_Resolve_CollapsesConcurrentMisses(t *testing.T) {
	c := newTestRefCache(t, Options{})
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("m")),
		Size:      1,
	}

	// Gate the upstream so all goroutines pile up on the same in-flight
	// resolve before the leader completes.
	release := make(chan struct{})
	var calls atomic.Int64
	upstream := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		calls.Add(1)
		<-release
		return desc, nil
	})

	ref, err := registry.ParseReference("ghcr.io/ns:v1")
	require.NoError(t, err)

	const N = 8
	var wg sync.WaitGroup
	results := make([]ociImageSpecV1.Descriptor, N)
	for i := range N {
		wg.Go(func() {
			got, err := c.Resolve(t.Context(), upstream, ref)
			require.NoError(t, err)
			results[i] = got
		})
	}
	// Give the goroutines time to coalesce on the singleflight key.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for i := range results {
		assert.Equal(t, desc, results[i], "resolver %d", i)
	}
	assert.EqualValues(t, 1, calls.Load(), "concurrent misses must collapse into a single upstream resolve")
}

func TestReferenceCache_Resolve_NamespacesIsolateCollisions(t *testing.T) {
	c := newTestRefCache(t, Options{})

	descA := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("a")),
		Size:      1,
	}
	descB := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("b")),
		Size:      1,
	}

	refA, err := registry.ParseReference("ghcr.io/foo:v1")
	require.NoError(t, err)

	refB, err := registry.ParseReference("ghcr.io/bar:v1")
	require.NoError(t, err)

	c.Add(refA, descA)
	c.Add(refB, descB)

	gotA, hitA := c.Lookup(refA)
	require.True(t, hitA)
	assert.Equal(t, descA, gotA)

	gotB, hitB := c.Lookup(refB)
	require.True(t, hitB)
	assert.Equal(t, descB, gotB)

	// And lookups in the wrong namespace miss.
	refOther, err := registry.ParseReference("ghcr.io/other:v1")
	require.NoError(t, err)
	_, hitMiss := c.Lookup(refOther)
	assert.False(t, hitMiss)
}

func TestReferenceCache_Resolve_ErrorNotCached(t *testing.T) {
	c := newTestRefCache(t, Options{})

	upstream := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		return ociImageSpecV1.Descriptor{}, errors.New("upstream boom")
	})

	ref, err := registry.ParseReference("ghcr.io/ns:ref-fail")
	require.NoError(t, err)

	_, err = c.Resolve(t.Context(), upstream, ref)
	require.Error(t, err)
	_, hit := c.Lookup(ref)
	assert.False(t, hit)
}

func TestReferenceCache_Resolve_NilReceiver_Passthrough(t *testing.T) {
	var c *ReferenceCache
	want := ociImageSpecV1.Descriptor{Digest: digest.FromBytes([]byte("p")), Size: 1, MediaType: "x"}
	upstream := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		return want, nil
	})

	ref, err := registry.ParseReference("ghcr.io/ns:x-y")
	require.NoError(t, err)

	got, err := c.Resolve(t.Context(), upstream, ref)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestReferenceCache_PersistsAcrossRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "refcache")

	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("persistent")),
		Size:      10,
	}

	c1, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)

	ref, err := registry.ParseReference("ghcr.io/owner/repo:v1")
	require.NoError(t, err)

	c1.Add(ref, desc)

	// The cache scopes to <Dir>/refs/ and shards by namespace; verify
	// that the namespace's snapshot file landed there.
	entries, err := os.ReadDir(filepath.Join(dir, referenceSubdir))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "reference snapshot must be written to disk")

	// Pretend the process restarts.
	c2, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)

	got, ok := c2.Lookup(ref)
	require.True(t, ok, "reference must be reseeded after restart")
	assert.Equal(t, desc, got)
}

func TestReferenceCache_RoundtripsArbitraryChars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "refcache")
	desc := ociImageSpecV1.Descriptor{
		MediaType: "y",
		Digest:    digest.FromBytes([]byte("x")),
		Size:      1,
	}
	// Tabs, newlines, quotes, unicode — all safe in a JSON snapshot.
	weird := "tag\nwith\ttabs/\"quotes\"/€"
	ref := registry.Reference{
		Registry:   "ghcr.io",
		Repository: "ns",
		Reference:  weird,
	}

	c1, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)
	c1.Add(ref, desc)

	c2, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)
	got, ok := c2.Lookup(ref)
	require.True(t, ok)
	assert.Equal(t, desc, got)
}

func TestReferenceCache_Load_NonNotExistError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not block root or windows")
	}
	c := newTestRefCache(t, Options{})
	// Drop read permission on the refs dir so os.ReadDir fails with a
	// non-NotExist error.
	require.NoError(t, os.Chmod(c.opts.Dir, 0))
	t.Cleanup(func() { _ = os.Chmod(c.opts.Dir, 0o700) })
	loaded, err := c.load()
	require.Error(t, err)
	assert.Zero(t, loaded)
}

func TestReferenceCache_Load_MissingFileReturnsZero(t *testing.T) {
	c := newTestRefCache(t, Options{})
	loaded, err := c.load()
	require.NoError(t, err)
	assert.Zero(t, loaded)
}

func TestReferenceCache_Add_LRUStaysConsistentWhenWriteFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dir-remove semantics differ on windows")
	}
	c := newTestRefCache(t, Options{})
	desc := ociImageSpecV1.Descriptor{
		MediaType: "y",
		Digest:    digest.FromBytes([]byte("x")),
		Size:      1,
	}
	// Replace the cache directory with a regular file so the temp-file
	// creation in writeFileAtomic fails.
	require.NoError(t, os.RemoveAll(c.opts.Dir))
	require.NoError(t, os.WriteFile(c.opts.Dir, nil, 0o600))

	ref, err := registry.ParseReference("ghcr.io/ns:ref-fail")
	require.NoError(t, err)

	c.Add(ref, desc)
	got, ok := c.Lookup(ref)
	require.True(t, ok, "in-memory entry must survive a disk-write failure")
	assert.Equal(t, desc, got)
}

func TestReferenceCache_Add_ConcurrentWritersAllVisible(t *testing.T) {
	dir := t.TempDir()
	c, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			ref := registry.Reference{
				Registry:   "ghcr.io",
				Repository: "ns",
				Reference:  string(rune('a' + i)),
			}
			c.Add(ref, ociImageSpecV1.Descriptor{
				Digest: digest.FromBytes([]byte{byte('a' + i)}),
				Size:   1,
			})
		})
	}
	wg.Wait()
	for i := range 4 {
		ref := registry.Reference{
			Registry:   "ghcr.io",
			Repository: "ns",
			Reference:  string(rune('a' + i)),
		}
		_, ok := c.Lookup(ref)
		assert.True(t, ok)
	}
}

func TestWriteFileAtomic_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	require.NoError(t, writeFileAtomic(dir, path, []byte("hello")))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), got)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, e.IsDir())
		assert.NotContains(t, e.Name(), ".write-")
	}
}

func TestWriteFileAtomic_TempDirMissing(t *testing.T) {
	err := writeFileAtomic(filepath.Join(t.TempDir(), "missing"), "irrelevant", []byte("x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create temp file")
}

func TestReferenceCache_Add_RewritesEvictedNamespaceSnapshot(t *testing.T) {
	r := require.New(t)
	dir := filepath.Join(t.TempDir(), "refcache")

	refA, err := registry.ParseReference("ghcr.io/a/repo:v1")
	r.NoError(err)
	refB, err := registry.ParseReference("ghcr.io/b/repo:v1")
	r.NoError(err)

	descA := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("a")),
		Size:      1,
	}
	descB := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("b")),
		Size:      1,
	}

	c1, err := NewReferenceCache(Options{Dir: dir, MaxEntries: 1})
	r.NoError(err)

	c1.Add(refA, descA)
	c1.Add(refB, descB) // triggers capacity eviction of A

	_, ok := c1.Lookup(refA)
	r.False(ok, "A must be evicted from memory")

	c2, err := NewReferenceCache(Options{Dir: dir, MaxEntries: 2})
	r.NoError(err)

	_, ok = c2.Lookup(refA)
	assert.False(t, ok, "snapshot-evicted reference must not survive restart")

	gotB, ok := c2.Lookup(refB)
	r.True(ok)
	assert.Equal(t, descB, gotB)
}
