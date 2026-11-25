package oci

import (
	"context"
	"encoding/json/v2"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"

	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
)

// signatureGraph hides access referrers from every walk over the graph. What
// remains reachable from a normalized manifest are its signature referrers.
// Access referrers are regenerated per repository and must not be copied.
//
// It embeds the interface, not a concrete store, so that a
// registry.ReferrerLister of the store is not promoted: ORAS prefers Referrers
// over Predecessors and would bypass the filter.
type signatureGraph struct{ content.ReadOnlyGraphStorage }

// Predecessors returns the referrers of desc without the access referrers.
//
// The artifact type a listing reports decides without a request. Listings
// omit it for an image index that has none, and referrers tag schema indexes
// are written by clients that may omit it, so a manifest or index listed
// without one is fetched. That fetch is not shared with a later copy.
func (g signatureGraph) Predecessors(ctx context.Context, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	predecessors, err := g.ReadOnlyGraphStorage.Predecessors(ctx, desc)
	if err != nil {
		return nil, err
	}
	kept := make([]ociImageSpecV1.Descriptor, 0, len(predecessors))
	for _, p := range predecessors {
		artifactType := p.ArtifactType
		if artifactType == "" && isImageManifestOrIndex(p.MediaType) {
			// A referrer kept without inspection would become a selectable
			// access referrer on the target, so a failed fetch fails the walk.
			raw, err := content.FetchAll(ctx, g.ReadOnlyGraphStorage, p)
			if err != nil {
				return nil, fmt.Errorf("failed to inspect referrer %s of %s: %w", p.Digest, desc.Digest, err)
			}
			if artifactType, err = manifestArtifactType(p.MediaType, raw); err != nil {
				return nil, fmt.Errorf("failed to inspect referrer %s of %s: %w", p.Digest, desc.Digest, err)
			}
		}
		if artifactType == ocidescriptor.ArtifactTypeAccessDescriptor {
			continue
		}
		kept = append(kept, p)
	}
	return kept, nil
}

// rejectAccessReferrers fails for a manifest or index that carries the access
// artifact type, before next is called. A listing can report another artifact
// type than the manifest has, which lets an access referrer pass
// [signatureGraph]. The target registry derives the type from the copied
// manifest, so the copy has to fail instead.
func rejectAccessReferrers(next findSuccessorsFunc) findSuccessorsFunc {
	if next == nil {
		next = content.Successors
	}
	return func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
		if !isImageManifestOrIndex(desc.MediaType) {
			return next(ctx, fetcher, desc)
		}
		// The copy proxy caches this fetch, and content.Successors reads the manifest from that cache.
		raw, err := content.FetchAll(ctx, fetcher, desc)
		if err != nil {
			return nil, err
		}
		artifactType, err := manifestArtifactType(desc.MediaType, raw)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect %s: %w", desc.Digest, err)
		}
		if artifactType == ocidescriptor.ArtifactTypeAccessDescriptor {
			return nil, fmt.Errorf("access referrer %s must not be copied: its artifact type %q is reserved for the access descriptor each repository generates itself", desc.Digest, artifactType)
		}
		return next(ctx, fetcher, desc)
	}
}

// isImageManifestOrIndex reports whether content of the media type can be an
// access referrer: the read path selects no other media type as one.
func isImageManifestOrIndex(mediaType string) bool {
	return mediaType == ociImageSpecV1.MediaTypeImageManifest || mediaType == ociImageSpecV1.MediaTypeImageIndex
}

// manifestArtifactType returns the artifact type registries report for the
// image manifest or image index raw. For an image manifest without
// artifactType that is the media type of its config.
func manifestArtifactType(mediaType string, raw []byte) (string, error) {
	var manifest struct {
		ArtifactType string `json:"artifactType"`
		Config       struct {
			MediaType string `json:"mediaType"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return "", err
	}
	if manifest.ArtifactType == "" && mediaType == ociImageSpecV1.MediaTypeImageManifest {
		return manifest.Config.MediaType, nil
	}
	return manifest.ArtifactType, nil
}
