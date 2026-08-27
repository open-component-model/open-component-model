package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

// OCILayerResourceStream is a ResourceStream over a single layer blob. No network
// I/O occurs at construction time.
//
// Materialize yields the layer content itself rather than the OCI layout tar
// OCIResourceStream produces, because a layer is a plain blob with no manifest a
// layout index could point at.
type OCILayerResourceStream struct {
	content.ReadOnlyGraphStorage
	Descriptor ocispec.Descriptor
}

var _ ResourceStream = (*OCILayerResourceStream)(nil)

func (s *OCILayerResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

// Materialize fetches the layer and buffers it into a blob. The returned blob
// carries the descriptor digest, so reading it verifies the content against it.
func (s *OCILayerResourceStream) Materialize(ctx context.Context) (b blob.ReadOnlyBlob, err error) {
	reader, err := s.Fetch(ctx, s.Descriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer %q: %w", s.Descriptor.Digest, err)
	}
	defer func() {
		err = errors.Join(err, reader.Close())
	}()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read layer %q: %w", s.Descriptor.Digest, err)
	}
	if int64(len(data)) != s.Descriptor.Size {
		return nil, fmt.Errorf("layer %q has size %d, but the descriptor declares %d", s.Descriptor.Digest, len(data), s.Descriptor.Size)
	}

	opts := []inmemory.MemoryBlobOption{
		inmemory.WithSize(s.Descriptor.Size),
		inmemory.WithDigest(s.Descriptor.Digest.String()),
	}
	// An empty media type would overwrite the default the blob applies on its own.
	if s.Descriptor.MediaType != "" {
		opts = append(opts, inmemory.WithMediaType(s.Descriptor.MediaType))
	}
	return inmemory.New(bytes.NewReader(data), opts...), nil
}
