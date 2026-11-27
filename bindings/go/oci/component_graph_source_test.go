package oci_test

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// fetchCountingResolver resolves the stores of a CTF and counts what is fetched from them, per
// digest. Nothing is cached between the repository and these stores.
type fetchCountingResolver struct {
	*ocictf.Store

	mu      sync.Mutex
	fetches map[digest.Digest]int
}

func (r *fetchCountingResolver) StoreForReference(ctx context.Context, reference string) (spec.Store, error) {
	store, err := r.Store.StoreForReference(ctx, reference)
	if err != nil {
		return nil, err
	}
	return fetchCountingStore{Store: store, resolver: r}, nil
}

type fetchCountingStore struct {
	spec.Store
	resolver *fetchCountingResolver
}

func (s fetchCountingStore) Fetch(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	s.resolver.mu.Lock()
	s.resolver.fetches[desc.Digest]++
	s.resolver.mu.Unlock()
	return s.Store.Fetch(ctx, desc)
}

func TestAddComponentVersionFromGraph_FetchesNormalizedManifestOnce(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	sourceCTF := newCTFStore(t)
	writer := Repository(t, ocictf.WithCTF(sourceCTF), oci.WithComponentVersionLayout(oci.LayoutNormalized))
	r.NoError(writer.AddComponentVersion(ctx, normDescriptor()))
	store, ref := componentStore(t, ctx, sourceCTF, normComponent, normVersion)
	normalized, err := store.Resolve(ctx, ref)
	r.NoError(err)

	counting := &fetchCountingResolver{Store: sourceCTF, fetches: map[digest.Digest]int{}}
	source := Repository(t, oci.WithResolver(counting))
	target := Repository(t, ocictf.WithCTF(newCTFStore(t)), oci.WithComponentVersionLayout(oci.LayoutNormalized))

	graph, err := source.DownloadComponentVersionGraph(ctx, normComponent, normVersion)
	r.NoError(err)
	r.NoError(target.AddComponentVersionFromGraph(ctx, normDescriptor(), graph))

	r.Equal(1, counting.fetches[normalized.Digest],
		"opening the graph, the checks before the copy and the copy must share one request for the normalized manifest")
	got, err := target.GetComponentVersion(ctx, normComponent, normVersion)
	r.NoError(err)
	r.Equal(normComponent, got.Component.Name)
}
