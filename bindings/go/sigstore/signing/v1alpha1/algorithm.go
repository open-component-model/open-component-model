package v1alpha1

// SignatureAlgorithm identifies an OCM-versioned Sigstore signing algorithm.
//
// The Algorithm captures the domain contract of the signing flow — what
// goes into the bundle, which cosign conventions apply, how verification is
// performed — and evolves independently from the on-the-wire bundle MediaType.
//
// +ocm:jsonschema-gen:enum=Sigstore/v1alpha1
// +ocm:jsonschema-gen:enum:deprecated=sigstore
type SignatureAlgorithm string

const (
	// AlgorithmSigstoreV1Alpha1 is the first generation of the OCM Sigstore
	// signing flow, implemented on top of cosign v3.
	AlgorithmSigstoreV1Alpha1 SignatureAlgorithm = "Sigstore/v1alpha1"

	// AlgorithmSigstoreLegacy is the bare wire value emitted by OCM CLIs
	// before the versioned identifier was introduced. Accepted on verify as
	// an alias for AlgorithmSigstoreV1Alpha1; never produced on sign.
	AlgorithmSigstoreLegacy SignatureAlgorithm = "sigstore"

	// AlgorithmSigstoreDefault is the algorithm the handler picks when
	// SignConfig.SignatureAlgorithm is empty. Bumping this default is a
	// breaking change.
	AlgorithmSigstoreDefault = AlgorithmSigstoreV1Alpha1

	// MediaTypeSigstoreBundle is the Sigstore protobuf bundle wire format
	// produced and accepted by this handler.
	MediaTypeSigstoreBundle = "application/vnd.dev.sigstore.bundle.v0.3+json"
)

// IsKnownAlgorithm reports whether the handler implements the given algorithm.
// AlgorithmSigstoreLegacy is accepted as an alias for AlgorithmSigstoreV1Alpha1.
func IsKnownAlgorithm(alg SignatureAlgorithm) bool {
	return alg == AlgorithmSigstoreV1Alpha1 || alg == AlgorithmSigstoreLegacy
}

// IsAcceptableMediaType reports whether the handler can read a bundle with
// this MediaType. Independent of Algorithm: both checks must pass on verify,
// but they are evaluated separately and can evolve on different schedules.
func IsAcceptableMediaType(mt string) bool {
	return mt == MediaTypeSigstoreBundle
}
