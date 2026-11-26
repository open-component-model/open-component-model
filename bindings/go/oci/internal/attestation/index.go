package attestation

import (
	"cmp"
	"context"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"

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
func DiscoverAllFromRoot(ctx context.Context, store spec.Store, root ociImageSpecV1.Descriptor, opts ...repository.AttestationOption) ([]repository.Attestation, error) {
	if !IsIndex(root.MediaType) {
		return nil, nil
	}

	includeData := repository.NewAttestationOptions(opts...).IncludeData

	var index ociImageSpecV1.Index
	if err := FetchJSON(ctx, store, root, &index); err != nil {
		return nil, fmt.Errorf("reading image index failed: %w", err)
	}

	imageList, attestations := partitionBuildKitIndex(index)
	images := make(map[string]ociImageSpecV1.Descriptor, len(imageList))
	for _, img := range imageList {
		images[img.Digest.String()] = img
	}

	var out []repository.Attestation
	for _, att := range attestations {
		var manifest ociImageSpecV1.Manifest
		if err := FetchJSON(ctx, store, att, &manifest); err != nil {
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
			a := repository.Attestation{
				Source:        repository.AttestationSourceOCIIndex,
				PredicateType: layer.Annotations[AnnotationInTotoPredicateType],
				MediaType:     layer.MediaType,
				Platform:      platform,
				Digest:        image.Digest.String(),
				ID:            layer.Digest.String(),
				ContentDigest: layer.Digest.String(),
				CreatedAt:     createdAt,
				Annotations:   manifest.Annotations,
			}
			if includeData {
				// The full in-toto statement, byte-for-byte (envelope included), not just
				// the predicate: a download must preserve exactly what was published.
				data, err := content.FetchAll(ctx, store, layer)
				if err != nil {
					return nil, fmt.Errorf("fetching in-toto statement layer %s failed: %w", layer.Digest, err)
				}
				a.Data = data
			}
			out = append(out, a)
		}
	}
	return out, nil
}

// partitionBuildKitIndex splits the manifests of a BuildKit image index into the platform
// images and the attestation manifests that describe them. An attestation manifest is one
// tagged vnd.docker.reference.type=attestation-manifest; it is kept only when it also
// carries the vnd.docker.reference.digest of the image it attests, so one without is
// dropped rather than mistaken for an image.
func partitionBuildKitIndex(index ociImageSpecV1.Index) (images, attestations []ociImageSpecV1.Descriptor) {
	for _, entry := range index.Manifests {
		if entry.Annotations[AnnotationDockerReferenceType] == ReferenceTypeAttestationManifest {
			if entry.Annotations[AnnotationDockerReferenceDigest] != "" {
				attestations = append(attestations, entry)
			}
			continue
		}
		images = append(images, entry)
	}
	return images, attestations
}
