package cache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
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
	"oras.land/oras-go/v2/content"
)

// fakeReadOnly is a minimal in-memory [content.ReadOnlyStorage] used
// to drive Fetch tests without pulling in a full registry.
type fakeReadOnly struct {
	mu      sync.Mutex
	blobs   map[digest.Digest][]byte
	media   map[digest.Digest]string
	fetches atomic.Int64
	exists  atomic.Int64
}

func newFakeReadOnly() *fakeReadOnly {
	return &fakeReadOnly{blobs: map[digest.Digest][]byte{}, media: map[digest.Digest]string{}}
}

func (s *fakeReadOnly) put(mt string, data []byte) ociImageSpecV1.Descriptor {
	d := digest.FromBytes(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blobs[d] = data
	s.media[d] = mt
	return ociImageSpecV1.Descriptor{MediaType: mt, Digest: d, Size: int64(len(data))}
}

func (s *fakeReadOnly) Fetch(_ context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	s.fetches.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.blobs[target.Digest]
	if !ok {
		return nil, fmt.Errorf("not found: %s", target.Digest)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *fakeReadOnly) Exists(_ context.Context, target ociImageSpecV1.Descriptor) (bool, error) {
	s.exists.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.blobs[target.Digest]
	return ok, nil
}

// remove deletes a blob from the fake store, simulating registry removal.
func (s *fakeReadOnly) remove(dgst digest.Digest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blobs, dgst)
	delete(s.media, dgst)
}

var _ content.ReadOnlyStorage = (*fakeReadOnly)(nil)

func readAllAndClose(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	return got
}

func TestCache_Fetch_NilReceiver_Passthrough(t *testing.T) {
	var c *BlobCache
	base := newFakeReadOnly()
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, []byte("p"))
	rc, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	_ = readAllAndClose(t, rc)
	assert.EqualValues(t, 1, base.fetches.Load())
}

func TestCache_Fetch_ManifestCachesAcrossCalls(t *testing.T) {
	c := newTestCache(t, Options{})
	base := newFakeReadOnly()
	manifest := []byte(`{"schemaVersion":2}`)
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)

	rc1, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	assert.Equal(t, manifest, readAllAndClose(t, rc1))
	assert.EqualValues(t, 1, base.fetches.Load())

	rc2, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	assert.Equal(t, manifest, readAllAndClose(t, rc2))
	assert.EqualValues(t, 1, base.fetches.Load(), "second call must hit cache")
}

func TestCache_Fetch_LayerSkipsCache(t *testing.T) {
	c := newTestCache(t, Options{})
	base := newFakeReadOnly()
	layer := []byte("layer")
	desc := base.put(ociImageSpecV1.MediaTypeImageLayerGzip, layer)

	rc1, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	_ = readAllAndClose(t, rc1)
	rc2, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	_ = readAllAndClose(t, rc2)

	assert.EqualValues(t, 2, base.fetches.Load())
	assert.False(t, c.Has(desc.Digest))
}

func TestCache_Fetch_OversizeSkipsCache(t *testing.T) {
	c := newTestCache(t, Options{MaxBlobSize: 4})
	base := newFakeReadOnly()
	manifest := []byte("0123456789")
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)

	rc1, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	_ = readAllAndClose(t, rc1)
	rc2, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	_ = readAllAndClose(t, rc2)

	assert.EqualValues(t, 2, base.fetches.Load())
	assert.False(t, c.Has(desc.Digest))
}

func TestCache_Fetch_ConcurrentReadersGetEqualBytes(t *testing.T) {
	c := newTestCache(t, Options{})
	base := newFakeReadOnly()
	manifest := bytes.Repeat([]byte("m"), 4096)
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)

	const N = 8
	results := make([][]byte, N)
	var wg sync.WaitGroup
	for i := range N {
		i := i
		wg.Go(func() {
			rc, err := c.Fetch(t.Context(), base, desc)
			require.NoError(t, err)
			results[i] = readAllAndClose(t, rc)
		})
	}
	wg.Wait()

	for i := range results {
		assert.Equal(t, manifest, results[i], "reader %d", i)
	}
}

// gatedReadOnly blocks inside Fetch until release is closed. It
// signals on blocked (buffered, cap N) each time a caller enters the
// gate, so tests can wait for a precise number of goroutines to be
// parked before releasing.
type gatedReadOnly struct {
	*fakeReadOnly
	blocked chan struct{}
	release chan struct{}
}

func newGated(base *fakeReadOnly, cap int) *gatedReadOnly {
	return &gatedReadOnly{
		fakeReadOnly: base,
		blocked:      make(chan struct{}, cap),
		release:      make(chan struct{}),
	}
}

func (s *gatedReadOnly) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	s.blocked <- struct{}{}
	<-s.release
	return s.fakeReadOnly.Fetch(ctx, target)
}

// waitBlocked drains n tokens from s.blocked, confirming n goroutines
// have entered the gate.
func (s *gatedReadOnly) waitBlocked(t *testing.T, n int) {
	t.Helper()
	for range n {
		select {
		case <-s.blocked:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for goroutine %d/%d to block", n, n)
		}
	}
}

func TestCache_Fetch_CollapsesConcurrentMisses(t *testing.T) {
	c := newTestCache(t, Options{})
	base := newFakeReadOnly()
	manifest := bytes.Repeat([]byte("m"), 4096)
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)
	gated := newGated(base, 1) // only the singleflight leader enters the gate

	const N = 8
	results := make([][]byte, N)
	var wg sync.WaitGroup
	t.Logf("launching %d concurrent Fetch goroutines for the same digest", N)
	for i := range N {
		wg.Go(func() {
			rc, err := c.Fetch(t.Context(), gated, desc)
			require.NoError(t, err)
			results[i] = readAllAndClose(t, rc)
		})
	}
	// Singleflight elects exactly one leader to call upstream; that
	// goroutine parks inside gatedReadOnly.Fetch waiting for release.
	// The other N-1 callers block on the DoChan result channel.
	t.Log("waiting for the single upstream fetch goroutine to park at the gate")
	gated.waitBlocked(t, 1)
	t.Logf("fetch goroutine parked; releasing gate — expecting %d upstream calls: 1", N)
	close(gated.release)
	wg.Wait()
	t.Logf("all %d callers returned; upstream fetch count: %d", N, base.fetches.Load())

	for i := range results {
		assert.Equal(t, manifest, results[i], "reader %d", i)
	}
	assert.EqualValues(t, 1, base.fetches.Load(),
		"concurrent misses must collapse into a single upstream fetch")
	assert.True(t, c.Has(desc.Digest), "blob must be persisted after the collapsed fetch")
}

func TestCache_Fetch_UpstreamErrorPropagates(t *testing.T) {
	c := newTestCache(t, Options{})
	base := newFakeReadOnly()
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes([]byte("nope")),
		Size:      4,
	}
	_, err := c.Fetch(t.Context(), base, desc)
	require.Error(t, err)
	assert.False(t, c.Has(desc.Digest))
}

func TestDefaultAccept_AllBranches(t *testing.T) {
	tests := []struct {
		mt   string
		want bool
	}{
		{ociImageSpecV1.MediaTypeImageManifest, true},
		{ociImageSpecV1.MediaTypeImageIndex, true},
		{"application/vnd.docker.distribution.manifest.v2+json", true},
		{"application/vnd.docker.distribution.manifest.list.v2+json", true},
		{"application/vnd.ocm.software.component.config.v1+json", true},
		{"application/vnd.ocm.software.component-descriptor.v2+json", true},
		{"application/vnd.ocm.software.component-descriptor.v1+yaml+tar", true},
		{ociImageSpecV1.MediaTypeImageLayerGzip, false},
		{"application/octet-stream", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.mt, func(t *testing.T) {
			assert.Equal(t, tc.want, DefaultAccept(ociImageSpecV1.Descriptor{MediaType: tc.mt}))
		})
	}
}

// blockingReadOnly blocks inside Fetch until ready is closed. It
// signals on blocked (capacity 1) when it enters the wait, so the
// test can synchronise without sleeping.
type blockingReadOnly struct {
	*fakeReadOnly
	blocked chan struct{}
	ready   chan struct{}
}

func newBlocking(base *fakeReadOnly) *blockingReadOnly {
	return &blockingReadOnly{
		fakeReadOnly: base,
		blocked:      make(chan struct{}, 1),
		ready:        make(chan struct{}),
	}
}

func (s *blockingReadOnly) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	select {
	case s.blocked <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ready:
	}
	return s.fakeReadOnly.Fetch(ctx, target)
}

func (s *blockingReadOnly) waitBlocked(t *testing.T) {
	t.Helper()
	select {
	case <-s.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fetch goroutine to block")
	}
}

// TestCache_Fetch_CanceledLeaderFollowerStillReceives verifies that
// when the leader caller's context is canceled before the shared fetch
// completes, the fetch continues and a concurrent follower receives
// the result.
func TestCache_Fetch_CanceledLeaderFollowerStillReceives(t *testing.T) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})))
	c := newTestCache(t, Options{})
	base := newFakeReadOnly()
	manifest := []byte(`{"schemaVersion":2,"leader":true}`)
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)
	blocking := newBlocking(base)

	leaderCtx, leaderCancel := context.WithCancel(t.Context())

	var leaderErr error
	leaderDone := make(chan struct{})
	t.Log("leader: starting Fetch — singleflight will elect it to call upstream")
	go func() {
		defer close(leaderDone)
		_, leaderErr = c.Fetch(leaderCtx, blocking, desc)
		t.Logf("leader: Fetch returned: %v", leaderErr)
	}()

	// blockingReadOnly.Fetch signals on blocked before parking; this
	// confirms the singleflight goroutine is inside the upstream call
	// and won't complete until blocking.ready is closed.
	t.Log("waiting for singleflight goroutine to park inside upstream Fetch")
	blocking.waitBlocked(t)
	t.Log("singleflight goroutine parked; canceling leader context")
	leaderCancel()
	<-leaderDone
	require.ErrorIs(t, leaderErr, context.Canceled, "leader must return its cancellation")
	t.Log("leader returned context.Canceled — singleflight goroutine still blocked on ready")

	// The singleflight goroutine is still running (blocked on ready).
	// Start the follower; it calls DoChan and receives the same
	// in-flight result channel, or gets a cache hit if the fetch
	// completes first — either way it must succeed.
	type fetchResult struct {
		data []byte
		err  error
	}
	followerCh := make(chan fetchResult, 1)
	t.Log("follower: starting Fetch while singleflight goroutine is still in-flight")
	go func() {
		rc, err := c.Fetch(t.Context(), blocking, desc)
		if err != nil {
			t.Logf("follower: Fetch returned error: %v", err)
			followerCh <- fetchResult{err: err}
			return
		}
		data := readAllAndClose(t, rc)
		t.Logf("follower: Fetch succeeded (%d bytes)", len(data))
		followerCh <- fetchResult{data: data}
	}()
	t.Log("unblocking singleflight goroutine by closing ready")
	close(blocking.ready)

	res := <-followerCh
	require.NoError(t, res.err, "follower must succeed even after leader canceled")
	assert.Equal(t, manifest, res.data)
	assert.True(t, c.Has(desc.Digest), "blob must be persisted for the follower")
	t.Logf("blob persisted: %v", c.Has(desc.Digest))
}

// TestCache_Fetch_CanceledFollowerDoesNotAffectOthers verifies that a
// follower whose context is canceled before the shared fetch completes
// returns [context.Canceled] while the leader (and any other active
// caller) can still retrieve the result.
func TestCache_Fetch_CanceledFollowerDoesNotAffectOthers(t *testing.T) {
	c := newTestCache(t, Options{})
	base := newFakeReadOnly()
	manifest := []byte(`{"schemaVersion":2,"follower":true}`)
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)
	blocking := newBlocking(base)

	type leaderResult struct {
		data []byte
		err  error
	}
	leaderCh := make(chan leaderResult, 1)
	t.Log("leader: starting Fetch — will block inside upstream until ready is closed")
	go func() {
		rc, err := c.Fetch(t.Context(), blocking, desc)
		if err != nil {
			t.Logf("leader: Fetch returned error: %v", err)
			leaderCh <- leaderResult{err: err}
			return
		}
		data := readAllAndClose(t, rc)
		t.Logf("leader: Fetch succeeded (%d bytes)", len(data))
		leaderCh <- leaderResult{data: data}
	}()

	t.Log("waiting for singleflight goroutine to park inside upstream Fetch")
	blocking.waitBlocked(t)
	t.Log("singleflight goroutine parked; follower joining with a pre-canceled context")
	followerCtx, followerCancel := context.WithCancel(t.Context())
	followerCancel()
	_, followerErr := c.Fetch(followerCtx, blocking, desc)
	t.Logf("follower: Fetch returned: %v (singleflight goroutine still running)", followerErr)
	require.ErrorIs(t, followerErr, context.Canceled, "follower must return its cancellation")

	t.Log("unblocking singleflight goroutine by closing ready")
	close(blocking.ready)
	result := <-leaderCh
	require.NoError(t, result.err, "leader must succeed after follower canceled")
	assert.Equal(t, manifest, result.data)
	assert.True(t, c.Has(desc.Digest))
	t.Logf("blob persisted: %v; upstream fetch count: %d", c.Has(desc.Digest), base.fetches.Load())
}

func TestCache_Fetch_GetErrorFallsThrough(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not block root or windows")
	}
	c := newTestCache(t, Options{})
	manifest := []byte(`{"schemaVersion":2}`)
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifest),
		Size:      int64(len(manifest)),
	}
	_, err := c.populate(t.Context(), desc.Digest, desc.Size, bytes.NewReader(manifest))
	require.NoError(t, err)

	// Block read access to the algorithm dir so c.Get returns a non-
	// NotExist error and Fetch must fall through to upstream.
	algo := filepath.Dir(pathFor(c.opts.Dir, desc.Digest))
	require.NoError(t, os.Chmod(algo, 0))
	t.Cleanup(func() { _ = os.Chmod(algo, 0o700) })

	base := newFakeReadOnly()
	_ = base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)

	rc, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	got := readAllAndClose(t, rc)
	assert.Equal(t, manifest, got)
	assert.EqualValues(t, 1, base.fetches.Load())
}
