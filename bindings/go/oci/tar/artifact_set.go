package tar

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/ctf"
)

var _ content.Fetcher = (*ctf.ArtifactSet)(nil)

// VerifyArtifactSet reads an OCM v1 artifact set, the format OCM v1 stores OCI artifacts in
// local blobs, checks every blob reachable from its main artifact against its digest and returns
// that artifact.
func VerifyArtifactSet(ctx context.Context, b blob.ReadOnlyBlob) (ociImageSpecV1.Descriptor, error) {
	set, err := ctf.NewArtifactSetFromBlob(b)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to read artifact set: %w", err)
	}
	defer func() {
		if err := set.Close(); err != nil {
			slog.WarnContext(ctx, "failed to close artifact set", "err", err)
		}
	}()

	main, err := artifactSetMain(ctx, set)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	if err := VerifyGraph(ctx, set, main); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to verify artifact set: %w", err)
	}
	return main, nil
}

// artifactSetMain returns the artifact the set annotates as its main one, else its single
// top-level artifact.
func artifactSetMain(ctx context.Context, set *ctf.ArtifactSet) (ociImageSpecV1.Descriptor, error) {
	idx := set.GetIndex()
	var candidates []ociImageSpecV1.Descriptor
	if main, ok := idx.Annotations[ctf.ArtifactSetMainAnnotation]; ok {
		candidates = slices.DeleteFunc(slices.Clone(idx.Manifests), func(desc ociImageSpecV1.Descriptor) bool {
			return desc.Digest.String() != main
		})
	} else {
		candidates = TopLevelArtifacts(ctx, set, idx.Manifests)
	}
	if len(candidates) != 1 {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("expected exactly one main artifact in artifact set, got %d", len(candidates))
	}
	return candidates[0], nil
}
