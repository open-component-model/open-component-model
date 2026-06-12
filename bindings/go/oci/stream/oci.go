package stream

import (
	"context"
	"log/slog"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// OCIResourceStream wraps a content.ReadOnlyGraphStorage (typically a remote.Repository)
// and a resolved root descriptor. No network I/O occurs at construction time.
// Tags are OCI reference tags applied to the layout during Materialize
// (passed to tar.CopyToOCILayoutOptions). For remote refs they should be the
// full ImageReference string so the caller can resolve the layout by that same key.
type OCIResourceStream struct {
	content.ReadOnlyGraphStorage
	Descriptor ocispec.Descriptor
	CopyOpts   oras.CopyGraphOptions
	TempDir    string
	Tags       []string
}

var (
	_ ResourceStream               = (*OCIResourceStream)(nil)
	_ content.ReadOnlyGraphStorage = (*OCIResourceStream)(nil)
)

func (s *OCIResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

// Predecessors reports the root's ownership referrers (ADR 0016), discovered
// from the wrapped store, so an oras.ExtendedCopyGraph carries them along.
// Other nodes report none; referrer-discovery failures are logged and treated
// as none, so they never fail an otherwise-healthy transfer.
func (s *OCIResourceStream) Predecessors(ctx context.Context, node ocispec.Descriptor) ([]ocispec.Descriptor, error) {
	if !content.Equal(node, s.Root()) {
		return nil, nil
	}
	refs, err := registry.Referrers(ctx, s.ReadOnlyGraphStorage, node, annotations.OwnershipArtifactType)
	if err != nil {
		slogcontext.Log(ctx, slog.LevelWarn, "failed listing ownership referrers; continuing without them", slog.Any("err", err))
		return nil, nil
	}
	return refs, nil
}

func (s *OCIResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
	// Include the root's ownership referrers in the layout so they travel to a
	// transfer target.
	referrers, err := s.Predecessors(ctx, s.Descriptor)
	if err != nil {
		return nil, err
	}
	return tar.CopyToOCILayoutInMemory(ctx, s.ReadOnlyGraphStorage, s.Descriptor, tar.CopyToOCILayoutOptions{
		CopyGraphOptions: s.CopyOpts,
		Tags:             s.Tags,
		TempDir:          s.TempDir,
		Referrers:        referrers,
	})
}
