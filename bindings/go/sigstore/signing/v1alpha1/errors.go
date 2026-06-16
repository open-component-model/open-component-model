package v1alpha1

import "errors"

// Sentinel errors returned (wrapped) by the sigstore handler and SignConfig.Validate.
// Callers can distinguish policy-violation classes via errors.Is.
var (
	ErrAlgorithmRequired     = errors.New("signature.Algorithm is required for sigstore verification")
	ErrUnknownAlgorithm      = errors.New("unknown sigstore algorithm")
	ErrUnacceptableMediaType = errors.New("unsupported media type for sigstore verification")
)
