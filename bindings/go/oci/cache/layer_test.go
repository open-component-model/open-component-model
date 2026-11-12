package cache

import (
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestCache_Fetch_AdmittedLayerIsCached(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{Accept: layerAccept(8)})
	base := newFakeReadOnly()
	desc := base.put(ociImageSpecV1.MediaTypeImageLayer, []byte("layer"))

	fetchTwice(t, c, base, desc)

	r.EqualValues(1, base.fetches.Load(), "the second fetch must be served from the cache")
	r.True(c.Has(desc.Digest))
}

func TestCache_Fetch_LayerNotAdmittedReachesUpstream(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		size func(int64) int64
	}{
		{name: "above the limit", data: []byte("0123456789"), size: func(n int64) int64 { return n }},
		{name: "unknown size", data: []byte("layer"), size: func(int64) int64 { return 0 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			c := newTestCache(t, Options{Accept: layerAccept(8)})
			base := newFakeReadOnly()
			desc := base.put(ociImageSpecV1.MediaTypeImageLayer, tc.data)
			desc.Size = tc.size(desc.Size)

			fetchTwice(t, c, base, desc)

			r.EqualValues(2, base.fetches.Load())
			r.False(c.Has(desc.Digest))
		})
	}
}

func TestBlobCache_Always_AdmittedLayerNeedsServedRecordAndExists(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{RemotePolicy: RemotePolicyAlways, Accept: layerAccept(64)})
	first := newFakeRepository("registry.example", "first")
	second := newFakeRepository("registry.example", "second")
	layer := []byte("layer-bytes")
	desc := first.put(ociImageSpecV1.MediaTypeImageLayer, layer)
	second.put(ociImageSpecV1.MediaTypeImageLayer, layer)

	fetchTwice(t, c, first, desc)
	r.EqualValues(1, first.fetches.Load(), "the hit must be served from the cache")
	r.EqualValues(1, first.exists.Load(), "the hit must check Exists")

	rc, err := c.Fetch(t.Context(), second, desc)
	r.NoError(err)
	r.Equal(layer, readAllAndClose(t, rc))
	r.EqualValues(1, second.fetches.Load(), "a repository that never served the layer must fetch it")
	r.Zero(second.exists.Load())

	first.remove(desc.Digest)
	_, err = c.Fetch(t.Context(), first, desc)
	r.Error(err, "a hit must not be served once the repository denies access")
}
