package v1alpha1

import "errors"

// SignatureAlgorithm is the OCM-versioned Notation signing algorithm.
//
// Accepted values:
//   - [AlgorithmNotationV1Alpha1] — canonical wire value "Notation/v1alpha1".
//     Use this for new signatures; [AlgorithmNotationDefault] points here.
//
// +ocm:jsonschema-gen:enum=Notation/v1alpha1
type SignatureAlgorithm string

const (
	// AlgorithmNotationV1Alpha1 is the first generation of the OCM Notation
	// signing flow, implemented on top of the notation-go blob API.
	AlgorithmNotationV1Alpha1 SignatureAlgorithm = "Notation/v1alpha1"

	// AlgorithmNotationDefault is the algorithm the handler picks when
	// SignConfig.SignatureAlgorithm is empty. Bumping this default is a
	// breaking change.
	AlgorithmNotationDefault = AlgorithmNotationV1Alpha1

	// MediaTypeJWSEnvelope is the JWS signature envelope wire format
	// ("application/jose+json"). It is the default envelope produced on sign.
	// The literal mirrors github.com/notaryproject/notation-core-go/signature/jws.MediaTypeEnvelope.
	MediaTypeJWSEnvelope = "application/jose+json"

	// MediaTypeCOSEEnvelope is the COSE signature envelope wire format
	// ("application/cose").
	// The literal mirrors github.com/notaryproject/notation-core-go/signature/cose.MediaTypeEnvelope.
	MediaTypeCOSEEnvelope = "application/cose"
)

// ErrUnknownAlgorithm is returned when SignConfig.SignatureAlgorithm or the
// algorithm of a signature being verified is set to a value the handler does
// not implement.
var ErrUnknownAlgorithm = errors.New("unknown notation algorithm")
