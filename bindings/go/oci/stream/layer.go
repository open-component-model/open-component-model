package stream

import (
	"context"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// OCILayerResourceStream wraps a content.ReadOnlyGraphStorage and a single
// layer descriptor. No network I/O occurs at construction time.
//
// A layer is a plain blob and not an OCI manifest, so it is the root of the
// graph and has no successors of its own. Consequently Materialize yields the
// layer content itself rather than the OCI layout tar OCIResourceStream
// produces: a bare blob cannot be the subject of a layout index, and callers
// asking for a layer want the bytes it addresses (a chart archive, a file, ...).
type OCILayerResourceStream struct {
	content.ReadOnlyGraphStorage
	Descriptor ocispec.Descriptor
}

var _ ResourceStream = (*OCILayerResourceStream)(nil)

func (s *OCILayerResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

// Materialize fetches the layer and buffers it into a blob. The descriptor's
// digest is handed to the blob so the content is verified against it while it
// is read.
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
