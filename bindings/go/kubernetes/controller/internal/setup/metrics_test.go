package setup_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
)

type blobs map[digest.Digest][]byte

func (b blobs) put(mediaType string, data []byte) ociImageSpecV1.Descriptor {
	d := digest.FromBytes(data)
	b[d] = data
	return ociImageSpecV1.Descriptor{MediaType: mediaType, Digest: d, Size: int64(len(data))}
}

func (b blobs) Fetch(_ context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b[desc.Digest])), nil
}

func (b blobs) Exists(_ context.Context, desc ociImageSpecV1.Descriptor) (bool, error) {
	_, ok := b[desc.Digest]
	return ok, nil
}

func (b blobs) ParseReference(reference string) (registry.Reference, error) {
	return registry.Reference{Registry: "registry.example", Repository: "repo", Reference: reference}, nil
}

func TestNewBlobCache_CountsFetches(t *testing.T) {
	r := require.New(t)
	c, err := setup.NewBlobCache(t.TempDir(), nil)
	r.NoError(err)

	upstream := blobs{}
	manifest := upstream.put(ociImageSpecV1.MediaTypeImageManifest, []byte(`{}`))
	layer := upstream.put(ociImageSpecV1.MediaTypeImageLayerGzip, []byte("layer"))

	count := func(kind, result string) float64 {
		m := &dto.Metric{}
		r.NoError(setup.BlobCacheFetches.WithLabelValues(kind, result).Write(m))
		return m.GetCounter().GetValue()
	}
	metadataMiss, metadataHit, blobBypass := count("metadata", "miss"), count("metadata", "hit"), count("blob", "bypass")

	for _, desc := range []ociImageSpecV1.Descriptor{manifest, manifest, layer} {
		rc, err := c.Fetch(t.Context(), upstream, desc)
		r.NoError(err)
		r.NoError(rc.Close())
	}

	r.InDelta(1, count("metadata", "miss")-metadataMiss, 0)
	r.InDelta(1, count("metadata", "hit")-metadataHit, 0)
	r.InDelta(1, count("blob", "bypass")-blobBypass, 0)
}
