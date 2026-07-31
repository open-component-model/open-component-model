package stream

import (
	"context"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// OCILayerResourceStream wraps a content.ReadOnlyGraphStorage and a single layer
// descriptor. No network I/O occurs at construction time.
//
// A layer is a plain blob, so it roots the graph with no successors of its own and
// cannot be the subject of a layout index. Materialize therefore yields the layer
// content itself rather than the OCI layout tar OCIResourceStream produces.
type OCILayerResourceStream struct {
	content.ReadOnlyGraphStorage
	Descriptor ocispec.Descriptor
}

var _ ResourceStream = (*OCILayerResourceStream)(nil)

func (s *OCILayerResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

// Materialize fetches the layer and buffers it into a blob. Passing the descriptor's
// digest along makes the blob verify the content against it as it is read.
func (s *OCILayerResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
	data, err := s.Fetch(ctx, s.Descriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer %q: %w", s.Descriptor.Digest, err)
	}
	return inmemory.New(data,
		inmemory.WithMediaType(s.Descriptor.MediaType),
		inmemory.WithSize(s.Descriptor.Size),
		inmemory.WithDigest(s.Descriptor.Digest.String()),
	), nil
}
