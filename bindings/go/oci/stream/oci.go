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

// OCIResourceStream wraps a content.ReadOnlyGraphStorage (typically a
// remote.Repository) and a resolved root descriptor. No network I/O occurs at
// construction time. Tags are OCI reference tags applied to the layout during
// Materialize (passed to tar.CopyToOCILayoutOptions). For remote refs they
// should be the full ImageReference string so the caller can resolve the
// layout by that same key.
type OCIResourceStream struct {
	content.ReadOnlyGraphStorage
	Descriptor ocispec.Descriptor
	CopyOpts   oras.CopyGraphOptions
	TempDir    string
	Tags       []string
}

var _ ResourceStream = (*OCIResourceStream)(nil)

func (s *OCIResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

// Materialize produces an OCI layout tar containing Descriptor and any ADR
// 0016 ownership referrers reachable from it via the source's Referrers API.
// Discovery is a best effort: a referrers-query hiccup is logged and the copy
// continues without them, since a referrer's subject edge points "backwards"
// at the root and a plain CopyGraph would never reach them anyway.
// oras.ExtendedCopyGraph at Depth 1 keeps the walk to direct referrers of
// Descriptor, never referrers-of-referrers.
func (s *OCIResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
	return tar.CopyToOCILayoutInMemory(ctx, s.ReadOnlyGraphStorage, s.Descriptor, tar.CopyToOCILayoutOptions{
		CopyGraphOptions: s.CopyOpts,
		Tags:             s.Tags,
		TempDir:          s.TempDir,
		Depth:            1,
		FindPredecessors: func(ctx context.Context, src content.ReadOnlyGraphStorage, desc ocispec.Descriptor) ([]ocispec.Descriptor, error) {
			refs, err := registry.Referrers(ctx, src, desc, annotations.OwnershipArtifactType)
			if err != nil {
				slogcontext.Log(ctx, slog.LevelWarn, "failed listing ownership referrers; continuing without them", slog.String("digest", desc.Digest.String()), slog.Any("err", err))
				return nil, nil
			}
			return refs, nil
		},
	})
}
