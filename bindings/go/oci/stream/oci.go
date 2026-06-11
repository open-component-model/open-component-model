package stream

import (
	"context"
	"fmt"
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

// OCIResourceStream wraps a content.ReadOnlyStorage (typically a remote.Repository)
// and a resolved root descriptor. No network I/O occurs at construction time.
// Tags are OCI reference tags applied to the layout during Materialize
// (passed to tar.CopyToOCILayoutOptions). For remote refs they should be the
// full ImageReference string so the caller can resolve the layout by that same key.
type OCIResourceStream struct {
	content.ReadOnlyStorage
	content.PredecessorFinder
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

// Predecessors makes the stream a content.ReadOnlyGraphStorage so it can be the
// source of an oras.ExtendedCopyGraph. It reports the ownership referrers (ADR
// 0016) of the root — discovered from the wrapped store via the Referrers API —
// as the root's predecessors, so the copy walks them up from Root and carries
// them along. Every other node reports none; a store that cannot answer referrer
// queries yields none; and a referrers-query hiccup must not fail an
// otherwise-healthy transfer, so it is logged and treated as none.
func (s *OCIResourceStream) Predecessors(ctx context.Context, node ocispec.Descriptor) ([]ocispec.Descriptor, error) {
	if !content.Equal(node, s.Root()) {
		return nil, nil
	}
	graphStore, ok := s.ReadOnlyStorage.(content.ReadOnlyGraphStorage)
	if !ok {
		slogcontext.Log(ctx, slog.LevelDebug, "source store does not support referrer discovery; skipping ownership referrers", slog.String("store", fmt.Sprintf("%T", s.ReadOnlyStorage)))
		return nil, nil
	}
	refs, err := registry.Referrers(ctx, graphStore, node, annotations.OwnershipArtifactType)
	if err != nil {
		slogcontext.Log(ctx, slog.LevelWarn, "failed listing ownership referrers; continuing without them", slog.Any("err", err))
		return nil, nil
	}
	return refs, nil
}

func (s *OCIResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
	// Pull the root's ownership referrers into the layout so they ride along to a
	// transfer target, mirroring the streaming-upload path that exposes them via
	// Predecessors to oras.ExtendedCopyGraph.
	referrers, err := s.Predecessors(ctx, s.Descriptor)
	if err != nil {
		return nil, err
	}
	return tar.CopyToOCILayoutInMemory(ctx, s.ReadOnlyStorage, s.Descriptor, tar.CopyToOCILayoutOptions{
		CopyGraphOptions: s.CopyOpts,
		Tags:             s.Tags,
		TempDir:          s.TempDir,
		Referrers:        referrers,
	})
}
