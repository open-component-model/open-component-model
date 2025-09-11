package v1alpha1

// SignatureEncodingPolicy defines how signatures are serialized and stored.
// Different policies trade off compactness, self-containment, and ease of verification.
type SignatureEncodingPolicy string

const (
	// SignatureEncodingPolicyDefault points to the default encoding policy.
	SignatureEncodingPolicyDefault = SignatureEncodingPolicyPlain

	// SignatureEncodingPolicyPlain encodes the signature as a plain hex string.
	//
	// Characteristics:
	//   - Most compact representation.
	//   - Not self-contained: verification requires the public key to be supplied
	//     from an external source (e.g. configuration, key management system).
	//   - No support for embedding or distributing certificate chains.
	SignatureEncodingPolicyPlain SignatureEncodingPolicy = "Plain"
)
