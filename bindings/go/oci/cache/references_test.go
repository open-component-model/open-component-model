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

	got, err := c.Resolve(t.Context(), upstream, "ns", "ghcr.io/owner/repo:v1")
	require.NoError(t, err)
	assert.Equal(t, desc, got)

	got2, err := c.Resolve(t.Context(), upstream, "ns", "ghcr.io/owner/repo:v1")
	require.NoError(t, err)
	assert.Equal(t, desc, got2)

	assert.EqualValues(t, 1, calls.Load(), "second Resolve must hit reference cache")
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
	c1.Add("ns", "tag:v1", desc)
	_, ok := c1.Lookup("ns", "tag:v1")
	require.True(t, ok)

	c1.Invalidate("ns", "tag:v1")
	_, ok = c1.Lookup("ns", "tag:v1")
	assert.False(t, ok, "entry must be gone from the in-memory LRU")

	// A restart must not resurrect the invalidated mapping.
	c2, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)
	_, ok = c2.Lookup("ns", "tag:v1")
	assert.False(t, ok, "invalidated mapping must not survive a restart")
}

func TestReferenceCache_Invalidate_UnknownEntryIsNoOp(t *testing.T) {
	c := newTestRefCache(t, Options{})
	c.Invalidate("ns", "never-added")
}

func TestReferenceCache_Invalidate_NilReceiver(t *testing.T) {
	var c *ReferenceCache
	c.Invalidate("ns", "ref")
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
	c1.Add("ns", "tag:v1", desc)

	// Backdate the persisted entry beyond the TTL by rewriting its
	// snapshot file with an old savedAt.
	snapPath := c1.pathForNamespace("ns")
	raw, err := os.ReadFile(snapPath)
	require.NoError(t, err)
	var snap referenceFileSnapshot
	require.NoError(t, json.Unmarshal(raw, &snap))
	entry := snap.References["tag:v1"]
	entry.SavedAt = time.Now().Add(-2 * time.Hour)
	snap.References["tag:v1"] = entry
	rewritten, err := json.Marshal(snap)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(snapPath, rewritten, 0o600))

	// A restart must honour the TTL and drop the stale mapping.
	c2, err := NewReferenceCache(Options{Dir: dir, TTL: time.Hour})
	require.NoError(t, err)
	_, ok := c2.Lookup("ns", "tag:v1")
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
	c1.Add("ns", "tag:v1", desc)

	c2, err := NewReferenceCache(Options{Dir: dir, TTL: time.Hour})
	require.NoError(t, err)
	got, ok := c2.Lookup("ns", "tag:v1")
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

	const N = 8
	var wg sync.WaitGroup
	results := make([]ociImageSpecV1.Descriptor, N)
	for i := range N {
		wg.Go(func() {
			got, err := c.Resolve(t.Context(), upstream, "ns", "tag:v1")
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
	c.Add("ghcr.io/foo", "v1", descA)
	c.Add("ghcr.io/bar", "v1", descB)

	gotA, hitA := c.Lookup("ghcr.io/foo", "v1")
	require.True(t, hitA)
	assert.Equal(t, descA, gotA)

	gotB, hitB := c.Lookup("ghcr.io/bar", "v1")
	require.True(t, hitB)
	assert.Equal(t, descB, gotB)

	// And lookups in the wrong namespace miss.
	_, hitMiss := c.Lookup("ghcr.io/other", "v1")
	assert.False(t, hitMiss)
}

func TestReferenceCache_Resolve_ErrorNotCached(t *testing.T) {
	c := newTestRefCache(t, Options{})

	upstream := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		return ociImageSpecV1.Descriptor{}, errors.New("upstream boom")
	})

	_, err := c.Resolve(t.Context(), upstream, "ns", "fail:v1")
	require.Error(t, err)
	_, hit := c.Lookup("ns", "fail:v1")
	assert.False(t, hit)
}

func TestReferenceCache_Resolve_NilReceiver_Passthrough(t *testing.T) {
	var c *ReferenceCache
	want := ociImageSpecV1.Descriptor{Digest: digest.FromBytes([]byte("p")), Size: 1, MediaType: "x"}
	upstream := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		return want, nil
	})
	got, err := c.Resolve(t.Context(), upstream, "ns", "x:y")
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
	c1.Add("ns", "ghcr.io/owner/repo:v1", desc)

	// The cache scopes to <Dir>/refs/ and shards by namespace; verify
	// that the namespace's snapshot file landed there.
	entries, err := os.ReadDir(filepath.Join(dir, referenceSubdir))
	require.NoError(t, err)
	require.NotEmpty(t, entries, "reference snapshot must be written to disk")

	// Pretend the process restarts.
	c2, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)

	got, ok := c2.Lookup("ns", "ghcr.io/owner/repo:v1")
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
	weird := "ghcr.io/owner/repo:tag\nwith\ttabs/\"quotes\"/€"

	c1, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)
	c1.Add("ns", weird, desc)

	c2, err := NewReferenceCache(Options{Dir: dir})
	require.NoError(t, err)
	got, ok := c2.Lookup("ns", weird)
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

	c.Add("ns", "ref:fail", desc)
	got, ok := c.Lookup("ns", "ref:fail")
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
			c.Add("ns", string(rune('a'+i)), ociImageSpecV1.Descriptor{
				Digest: digest.FromBytes([]byte{byte('a' + i)}),
				Size:   1,
			})
		})
	}
	wg.Wait()
	for i := range 4 {
		_, ok := c.Lookup("ns", string(rune('a'+i)))
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

func TestReferenceCache_NormalizeReference(t *testing.T) {
	c := newTestRefCache(t, Options{})
	tests := []struct {
		name      string
		namespace string
		reference string
		want      string
	}{
		{
			"empty namespace",
			"",
			"ghcr.io/owner/repo:v1",
			"ghcr.io/owner/repo:v1",
		},
		{
			"unmatched namespace",
			"ghcr.io/other/repo",
			"ghcr.io/owner/repo:v1",
			"ghcr.io/owner/repo:v1",
		},
		{
			"matched tag reference",
			"ghcr.io/owner/repo",
			"ghcr.io/owner/repo:v1",
			"v1",
		},
		{
			"matched digest reference",
			"ghcr.io/owner/repo",
			"ghcr.io/owner/repo@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		},
		{
			"partial matched namespace prefix without separator",
			"ghcr.io/owner/repo",
			"ghcr.io/owner/repository:v1",
			"ghcr.io/owner/repository:v1",
		},
		{
			"already stripped reference",
			"ghcr.io/owner/repo",
			"v1",
			"v1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := c.normalizeReference(tc.namespace, tc.reference)
			assert.Equal(t, tc.want, got)
		})
	}
}
