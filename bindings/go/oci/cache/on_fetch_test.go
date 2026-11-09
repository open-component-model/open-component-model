package cache

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

// fetchRecorder collects what [Options.OnFetch] reports.
type fetchRecorder struct {
	mu      sync.Mutex
	results []FetchResult
}

func (r *fetchRecorder) record(_ ociImageSpecV1.Descriptor, result FetchResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, result)
}

func (r *fetchRecorder) got() []FetchResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]FetchResult(nil), r.results...)
}

func TestOnFetch_Reports(t *testing.T) {
	for _, policy := range []RemotePolicy{RemotePolicyIfNotPresent, RemotePolicyAlways} {
		t.Run(string(policy), func(t *testing.T) {
			r := require.New(t)
			rec := &fetchRecorder{}
			c := newTestCache(t, Options{RemotePolicy: policy, MaxBlobSize: 8, OnFetch: rec.record})
			base := newFakeReadOnly()
			manifest := base.put(ociImageSpecV1.MediaTypeImageManifest, []byte(`{}`))
			layer := base.put(ociImageSpecV1.MediaTypeImageLayerGzip, []byte("layer"))
			oversized := base.put(ociImageSpecV1.MediaTypeImageManifest, []byte(`{"schemaVersion":2}`))

			for _, desc := range []ociImageSpecV1.Descriptor{manifest, manifest, layer, oversized} {
				rc, err := c.Fetch(t.Context(), base, desc)
				r.NoError(err)
				_ = readAllAndClose(t, rc)
			}

			r.Equal([]FetchResult{FetchResultMiss, FetchResultHit, FetchResultBypass, FetchResultBypass}, rec.got())
		})
	}
}

func TestOnFetch_Always_DeniedHitIsNotReported(t *testing.T) {
	r := require.New(t)
	rec := &fetchRecorder{}
	c := newTestCache(t, Options{RemotePolicy: RemotePolicyAlways, OnFetch: rec.record})
	base := newFakeReadOnly()
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, []byte(`{}`))

	rc, err := c.Fetch(t.Context(), base, desc)
	r.NoError(err)
	_ = readAllAndClose(t, rc)
	base.remove(desc.Digest)

	_, err = c.Fetch(t.Context(), base, desc)
	r.Error(err)
	r.Equal([]FetchResult{FetchResultMiss}, rec.got())
}

func TestOnFetch_FailedFetchIsNotReported(t *testing.T) {
	r := require.New(t)
	rec := &fetchRecorder{}
	c := newTestCache(t, Options{OnFetch: rec.record})
	base := newFakeReadOnly()
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, []byte(`{}`))
	base.remove(desc.Digest)

	_, err := c.Fetch(t.Context(), base, desc)
	r.Error(err)
	r.Empty(rec.got())
}

func TestOnFetch_NilHook(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{})
	base := newFakeReadOnly()
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, []byte(`{}`))

	for range 2 {
		rc, err := c.Fetch(t.Context(), base, desc)
		r.NoError(err)
		_ = readAllAndClose(t, rc)
	}
}

// gatedFetch blocks every upstream fetch until gate is closed.
type gatedFetch struct {
	*fakeReadOnly
	gate chan struct{}
}

func (g *gatedFetch) Fetch(ctx context.Context, d ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	<-g.gate
	return g.fakeReadOnly.Fetch(ctx, d)
}

func TestOnFetch_SharedFlightReportsOneMiss(t *testing.T) {
	r := require.New(t)
	rec := &fetchRecorder{}
	c := newTestCache(t, Options{OnFetch: rec.record})
	base := newFakeReadOnly()
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, []byte(`{}`))
	up := &gatedFetch{fakeReadOnly: base, gate: make(chan struct{})}

	const callers = 8
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			rc, err := c.Fetch(t.Context(), up, desc)
			if err == nil {
				_ = rc.Close()
			}
		})
	}
	time.Sleep(200 * time.Millisecond) // let every caller join the flight
	close(up.gate)
	wg.Wait()

	counts := map[FetchResult]int{}
	for _, res := range rec.got() {
		counts[res]++
	}
	r.EqualValues(1, base.fetches.Load())
	r.Equal(map[FetchResult]int{FetchResultMiss: 1, FetchResultHit: callers - 1}, counts,
		"only the caller that fetched from upstream is a miss")
}
