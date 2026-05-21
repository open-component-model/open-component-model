package v1alpha1

import "slices"

// SignatureAlgorithm identifies an OCM-versioned Sigstore signing algorithm.
//
// The Algorithm captures the *fachliche* contract of the signing flow — what
// goes into the bundle, which cosign conventions apply, how verification is
// performed — and evolves independently from the on-the-wire bundle MediaType.
// A single Algorithm version may accept several MediaTypes when the wire
// format changes without breaking the contract.
//
// +ocm:jsonschema-gen:enum=Sigstore/v1alpha1
type SignatureAlgorithm string

const (
	// AlgorithmSigstoreV1Alpha1 is the first generation of the OCM Sigstore
	// signing flow. It is implemented on top of cosign v3 and produces
	// Sigstore protobuf bundles (v0.3) that contain the short-lived Fulcio
	// certificate in the verification material. The Rekor inclusion proof is
	// stored alongside.
	AlgorithmSigstoreV1Alpha1 SignatureAlgorithm = "Sigstore/v1alpha1"

	// AlgorithmSigstoreDefault is the algorithm the handler picks when
	// SignConfig.SignatureAlgorithm is empty. Bumping this default to a future
	// version is a breaking change and requires a major version bump of this
	// module.
	AlgorithmSigstoreDefault = AlgorithmSigstoreV1Alpha1
)

const (
	// MediaTypeSigstoreBundleV03 is the Sigstore protobuf bundle v0.3 wire format,
	// the only MediaType produced and accepted by AlgorithmSigstoreV1Alpha1.
	MediaTypeSigstoreBundleV03 = "application/vnd.dev.sigstore.bundle.v0.3+json"

	// MediaTypeSigstoreBundle is the default MediaType for AlgorithmSigstoreV1Alpha1.
	// Kept as an alias for the v0.3 constant so callers that want "the current default"
	// can refer to it without binding to a specific wire-format version.
	MediaTypeSigstoreBundle = MediaTypeSigstoreBundleV03
)

// algorithmMediaTypes lists the on-the-wire bundle MediaTypes the handler
// accepts for a given Algorithm version. The first entry is the default the
// handler emits during signing.
//
// Adding a new MediaType to a known Algorithm here is a non-breaking
// change, e.g. when cosign starts emitting bundles in a newer wire format
// that is semantically compatible with our existing flow.
var algorithmMediaTypes = map[SignatureAlgorithm][]string{
	AlgorithmSigstoreV1Alpha1: {MediaTypeSigstoreBundleV03},
}

// IsKnownAlgorithm reports whether alg is a registered Sigstore algorithm version.
func IsKnownAlgorithm(alg SignatureAlgorithm) bool {
	_, ok := algorithmMediaTypes[alg]
	return ok
}

// AcceptableMediaTypes returns the bundle MediaTypes the handler accepts for
// the given Algorithm. Returns nil for unknown algorithms.
func AcceptableMediaTypes(alg SignatureAlgorithm) []string {
	mts, ok := algorithmMediaTypes[alg]
	if !ok {
		return nil
	}
	return slices.Clone(mts)
}

// DefaultBundleMediaType returns the MediaType the handler writes into
// SignatureInfo.MediaType when signing with the given Algorithm. Returns ""
// for unknown algorithms.
func DefaultBundleMediaType(alg SignatureAlgorithm) string {
	mts := algorithmMediaTypes[alg]
	if len(mts) == 0 {
		return ""
	}
	return mts[0]
}

// IsAcceptableMediaType reports whether mt is in AcceptableMediaTypes(alg).
func IsAcceptableMediaType(alg SignatureAlgorithm, mt string) bool {
	return slices.Contains(algorithmMediaTypes[alg], mt)
}
