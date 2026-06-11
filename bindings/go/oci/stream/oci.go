package stream

import (
	"context"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// OCIResourceStream wraps a content.ReadOnlyStorage (typically a remote.Repository)
// and a resolved root descriptor. No network I/O occurs at construction time.
// Tags are OCI reference tags applied to the layout during Materialize
// (passed to tar.CopyToOCILayoutOptions). For remote refs they should be the
// full ImageReference string so the caller can resolve the layout by that same key.
//
// FindPredecessors and Depth, when set, drive an oras.ExtendedCopyGraph from
// Descriptor during Materialize so any predecessors the callback returns (e.g.
// ADR 0016 ownership referrers) ride along into the layout. Discovery is lazy:
// nothing is queried until Materialize runs. The streaming upload path
// (oras.ExtendedCopyGraph against the stream) sets its own FindPredecessors at
// the call site and does not consult these fields.
type OCIResourceStream struct {
	content.ReadOnlyGraphStorage
	Descriptor ocispec.Descriptor
	CopyOpts   oras.CopyGraphOptions
	TempDir    string
	Tags       []string
	// I was also wondering whether I can get rid of this here entirely?
	FindPredecessors func(ctx context.Context, src content.ReadOnlyGraphStorage, desc ocispec.Descriptor) ([]ocispec.Descriptor, error)
	Depth            int
}

var _ ResourceStream = (*OCIResourceStream)(nil)

func (s *OCIResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

func (s *OCIResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
	return tar.CopyToOCILayoutInMemory(ctx, s.ReadOnlyGraphStorage, s.Descriptor, tar.CopyToOCILayoutOptions{
		CopyGraphOptions: s.CopyOpts,
		Tags:             s.Tags,
		TempDir:          s.TempDir,
		FindPredecessors: s.FindPredecessors,
		Depth:            s.Depth,
	})
}
