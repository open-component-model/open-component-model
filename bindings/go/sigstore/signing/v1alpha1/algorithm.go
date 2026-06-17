package v1alpha1

import "errors"

// SignatureAlgorithm is the OCM-versioned Sigstore signing algorithm.
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

// ErrUnknownAlgorithm is returned when SignConfig.SignatureAlgorithm or the
// algorithm of a signature being verified is set to a value the handler does
// not implement.
var ErrUnknownAlgorithm = errors.New("unknown sigstore algorithm")
