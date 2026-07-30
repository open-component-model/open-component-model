package stream

import (
	"context"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// OCIResourceStream wraps a content.ReadOnlyGraphStorage (typically a
// remote.Repository) and a resolved root descriptor. No network I/O occurs at
// construction time. Tags are OCI reference tags applied to the layout during
// Materialize (passed to tar.CopyToOCILayoutOptions). For remote refs they
// should be the full ImageReference string so the caller can resolve the
// layout by that same key.
//
// ExtendedCopyOpts drives Materialize's oras.ExtendedCopyGraph. The zero value
// uses oras's defaults: src.Predecessors and unbounded Depth, so every
// referrer of Descriptor rides along into the layout.
type OCIResourceStream struct {
	content.ReadOnlyGraphStorage
	Descriptor       ocispec.Descriptor
	ExtendedCopyOpts oras.ExtendedCopyGraphOptions
	TempDir          string
	Tags             []string
}

var _ ResourceStream = (*OCIResourceStream)(nil)

func (s *OCIResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

// Materialize produces an OCI layout tar containing Descriptor and the
// predecessors reachable via ExtendedCopyOpts. The walk is entirely controlled
// by the caller via ExtendedCopyOpts.
func (s *OCIResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
	return tar.CopyToOCILayoutInMemory(ctx, s.ReadOnlyGraphStorage, s.Descriptor, tar.CopyToOCILayoutOptions{
		ExtendedCopyGraphOptions: s.ExtendedCopyOpts,
		Tags:                     s.Tags,
		TempDir:                  s.TempDir,
	})
}
