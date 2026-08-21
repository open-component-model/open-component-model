package attestation

import (
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/repository"
)

// ToRepositoryPlatform converts an OCI platform to its technology agnostic equivalent.
func ToRepositoryPlatform(platform ociImageSpecV1.Platform) repository.Platform {
	return repository.Platform{
		OS:           platform.OS,
		Architecture: platform.Architecture,
		Variant:      platform.Variant,
		OSVersion:    platform.OSVersion,
		OSFeatures:   platform.OSFeatures,
	}
}

// ToRepositorySBOMs converts discovered attestations to their technology agnostic
// equivalent, dropping the OCI specific detail a generic consumer cannot use. The
// in-toto layer digest becomes the document id, it is the only value guaranteed to be
// unique across the SBOMs of one resource.
func ToRepositorySBOMs(sboms []SBOM) []repository.SBOM {
	converted := make([]repository.SBOM, 0, len(sboms))
	for _, sbom := range sboms {
		converted = append(converted, repository.SBOM{
			Platform:      ToRepositoryPlatform(sbom.Platform),
			ID:            sbom.Layer.Digest.String(),
			Name:          sbom.Name,
			PredicateType: sbom.PredicateType,
			Data:          sbom.Predicate,
		})
	}
	return converted
}
