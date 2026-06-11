package cache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

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
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.blobs[target.Digest]
	return ok, nil
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
			assert.Equal(t, tc.want, DefaultAccept(tc.mt))
		})
	}
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
	_, err := c.Populate(t.Context(), desc.Digest, desc.Size, bytes.NewReader(manifest))
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
