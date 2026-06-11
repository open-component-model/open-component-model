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
	// DiscoverReferrers lazily lists the referrer manifests (e.g. ADR 0016
	// ownership referrers) of Descriptor that must travel with it. It is the single
	// discovery hook for both transfer paths: Materialize pulls the result into the
	// layout, and Predecessors exposes it to oras.ExtendedCopyGraph during a
	// streaming upload. Discovery is lazy — no network I/O until the stream is
	// consumed — and nil means no referrers travel.
	DiscoverReferrers func(ctx context.Context) ([]ocispec.Descriptor, error)
}

var _ ResourceStream = (*OCIResourceStream)(nil)

func (s *OCIResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

// Predecessors makes the stream a content.ReadOnlyGraphStorage so it can be the
// source of an oras.ExtendedCopyGraph. It reports the referrers that must travel
// with the root (via DiscoverReferrers) as the root's predecessors, so the copy
// walks them up from Root and carries them along. Every other node — and a stream
// with no DiscoverReferrers — reports none.
func (s *OCIResourceStream) Predecessors(ctx context.Context, node ocispec.Descriptor) ([]ocispec.Descriptor, error) {
	if s.DiscoverReferrers == nil || node.Digest != s.Descriptor.Digest {
		return nil, nil
	}
	return s.DiscoverReferrers(ctx)
}

func (s *OCIResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
	var referrers []ocispec.Descriptor
	if s.DiscoverReferrers != nil {
		var err error
		if referrers, err = s.DiscoverReferrers(ctx); err != nil {
			return nil, err
		}
	}
	return tar.CopyToOCILayoutInMemory(ctx, s.ReadOnlyStorage, s.Descriptor, tar.CopyToOCILayoutOptions{
		CopyGraphOptions: s.CopyOpts,
		Tags:             s.Tags,
		TempDir:          s.TempDir,
		Referrers:        referrers,
	})
}
