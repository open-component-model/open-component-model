package attestation

const (
	// AnnotationDockerReferenceType type of the entry. Set by BuildKit.
	AnnotationDockerReferenceType = "vnd.docker.reference.type"
	// AnnotationDockerReferenceDigest digest of the entry. Set by BuildKit.
	AnnotationDockerReferenceDigest = "vnd.docker.reference.digest"
	// ReferenceTypeAttestationManifest is the AnnotationDockerReferenceType value for
	// an attestation manifest.
	ReferenceTypeAttestationManifest = "attestation-manifest"

	// PlatformUnknown architecture and os placeholder set by BuildKit.
	PlatformUnknown = "unknown"

	// MediaTypeInTotoStatement in-toto Statement mediaType.
	MediaTypeInTotoStatement = "application/vnd.in-toto+json"
	// AnnotationInTotoPredicateType predicate types in the in-toto layer.
	AnnotationInTotoPredicateType = "in-toto.io/predicate-type"

	// PredicateTypeSPDX predicate type created by "docker buildx build --sbom=true".
	PredicateTypeSPDX = "https://spdx.dev/Document"
	// PredicateTypeCycloneDX CycloneDX SBOM predicate type.
	PredicateTypeCycloneDX = "https://cyclonedx.org/bom"

	// MediaTypeSPDXJSON media type of SPDX document.
	MediaTypeSPDXJSON = "application/spdx+json"
	// MediaTypeCycloneDXJSON media type of CycloneDX document.
	MediaTypeCycloneDXJSON = "application/vnd.cyclonedx+json"

	// CoreSBOMName is the name BuildKit gives to the core SBOM. This is hardcoded
	// here at the moment: https://github.com/moby/buildkit/blob/master/frontend/attestations/sbom/sbom.go#L19
	// There is no other indication at the moment which SBOM document belongs to
	// the image and which describes the dependencies and which scans the content.
	// This is specifically a BuildKit dependency.
	//	// moby/buildkit/frontend/attestations/sbom/sbom.go
	//	CoreSBOMName    = "sbom"
	//	ExtraSBOMPrefix = CoreSBOMName + "-"
	CoreSBOMName = "sbom"
)
