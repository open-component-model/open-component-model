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
)
