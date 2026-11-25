package attestation

import (
	"cmp"
	"context"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/repository"
)

// DiscoverAllFromRoot lists the BuildKit in-toto attestations embedded in an image
// index, metadata-only: it reads the index and each attestation manifest, but never the
// in-toto statement blobs. One Attestation is returned per in-toto layer, carrying the
// predicate type from the layer annotation and the platform of the image it attests. It
// returns nil for a plain manifest (no index to walk) and does not filter by predicate,
// so SBOM and provenance and anything else BuildKit attached are all reported.
func DiscoverAllFromRoot(ctx context.Context, store spec.Store, root ociImageSpecV1.Descriptor) ([]repository.Attestation, error) {
	if !IsIndex(root.MediaType) {
		return nil, nil
	}

	var index ociImageSpecV1.Index
	if err := fetchJSON(ctx, store, root, &index); err != nil {
		return nil, fmt.Errorf("reading image index failed: %w", err)
	}

	images := make(map[string]ociImageSpecV1.Descriptor, len(index.Manifests))
	var attestations []ociImageSpecV1.Descriptor
	for _, entry := range index.Manifests {
		if entry.Annotations[AnnotationDockerReferenceType] == ReferenceTypeAttestationManifest &&
			entry.Annotations[AnnotationDockerReferenceDigest] != "" {
			attestations = append(attestations, entry)
			continue
		}
		images[entry.Digest.String()] = entry
	}

	var out []repository.Attestation
	for _, att := range attestations {
		var manifest ociImageSpecV1.Manifest
		if err := fetchJSON(ctx, store, att, &manifest); err != nil {
			return nil, fmt.Errorf("reading attestation manifest %s failed: %w", att.Digest, err)
		}

		image := images[att.Annotations[AnnotationDockerReferenceDigest]]
		var platform repository.Platform
		if image.Platform != nil {
			platform = identity.ToRepositoryPlatform(*image.Platform)
		}
		createdAt := cmp.Or(manifest.Annotations[ociImageSpecV1.AnnotationCreated], att.Annotations[ociImageSpecV1.AnnotationCreated])

		for _, layer := range manifest.Layers {
			if layer.MediaType != MediaTypeInTotoStatement {
				continue
			}
			out = append(out, repository.Attestation{
				Source:        repository.AttestationSourceOCIIndex,
				PredicateType: layer.Annotations[AnnotationInTotoPredicateType],
				MediaType:     layer.MediaType,
				Platform:      platform,
				Digest:        image.Digest.String(),
				ID:            layer.Digest.String(),
				CreatedAt:     createdAt,
				Annotations:   manifest.Annotations,
			})
		}
	}
	return out, nil
}
