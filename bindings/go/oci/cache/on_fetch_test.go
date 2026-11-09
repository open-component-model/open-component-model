package cache

import (
	"sync"
	"testing"

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
