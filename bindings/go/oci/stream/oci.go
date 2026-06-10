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
type OCIResourceStream struct {
	content.ReadOnlyStorage
	Descriptor ocispec.Descriptor
	CopyOpts   oras.CopyGraphOptions
	TempDir    string
	Tags       []string
	// ReferrerDescriptors are referrer manifests (e.g. ADR 0016 ownership
	// referrers) reachable from ReadOnlyStorage that must travel with Descriptor.
	// Exposed via [OCIResourceStream.Referrers].
	ReferrerDescriptors []ocispec.Descriptor
}

var _ ResourceStream = (*OCIResourceStream)(nil)

func (s *OCIResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

// Predecessors forwards to the underlying storage so the stream satisfies
// content.ReadOnlyGraphStorage, as oras.ExtendedCopyGraph requires of its source.
// It returns node's real parents (none if the storage can't report them).
func (s *OCIResourceStream) Predecessors(ctx context.Context, node ocispec.Descriptor) ([]ocispec.Descriptor, error) {
	finder, ok := s.ReadOnlyStorage.(content.PredecessorFinder)
	if !ok {
		return nil, nil
	}
	return finder.Predecessors(ctx, node)
}

func (s *OCIResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
	return tar.CopyToOCILayoutInMemory(ctx, s.ReadOnlyStorage, s.Descriptor, tar.CopyToOCILayoutOptions{
		CopyGraphOptions: s.CopyOpts,
		Tags:             s.Tags,
		TempDir:          s.TempDir,
		Referrers:        s.ReferrerDescriptors,
	})
}
