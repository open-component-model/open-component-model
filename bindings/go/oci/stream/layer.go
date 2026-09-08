package stream

import (
	"context"
	"errors"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// OCILayerResourceStream provides access to a single layer blob without an OCI layout.
// Construction does not fetch data.
type OCILayerResourceStream struct {
	content.ReadOnlyGraphStorage
	Descriptor ocispec.Descriptor
}

var _ ResourceStream = (*OCILayerResourceStream)(nil)

func (s *OCILayerResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

// Materialize fetches and verifies the layer for Repository.DownloadResource.
func (s *OCILayerResourceStream) Materialize(ctx context.Context) (b blob.ReadOnlyBlob, err error) {
	reader, err := s.Fetch(ctx, s.Descriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer %q: %w", s.Descriptor.Digest, err)
	}
	defer func() {
		err = errors.Join(err, reader.Close())
	}()

	opts := []inmemory.MemoryBlobOption{
		inmemory.WithDigest(s.Descriptor.Digest.String()),
	}
	// An empty media type would overwrite the default the blob applies on its own.
	if s.Descriptor.MediaType != "" {
		opts = append(opts, inmemory.WithMediaType(s.Descriptor.MediaType))
	}
	layer := inmemory.New(reader, opts...)
	if err := layer.Load(); err != nil {
		return nil, fmt.Errorf("failed to read layer %q: %w", s.Descriptor.Digest, err)
	}
	if layer.Size() != s.Descriptor.Size {
		return nil, fmt.Errorf("layer %q has size %d, but the descriptor declares %d", s.Descriptor.Digest, layer.Size(), s.Descriptor.Size)
	}
	return layer, nil
}
