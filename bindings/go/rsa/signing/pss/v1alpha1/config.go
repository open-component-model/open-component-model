package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// Type defines the type identifier for credential configurations
	Type = "RSASSA-PSS"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{}, runtime.NewVersionedType(Type, Version))
}

// Config represents the top-level configuration for the plugin manager.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type Config struct {
	Type runtime.Type `json:"type"`

	// SignatureEncodingPolicy defines the encoding policy to use for the signature once created.
	SignatureEncodingPolicy SignatureEncodingPolicy `json:"signatureEncoding"`
}

type SignatureEncodingPolicy string

const (
	// SignatureEncodingPolicyPlain declares that the signature should be stored as a plain hex string.
	// While this is the most compact representation, it is not self contained, so verifying the signature
	// requires the public key to be available from an external source.
	SignatureEncodingPolicyPlain SignatureEncodingPolicy = "Plain"

	// SignatureEncodingPolicyPEM stores the signature as a single PEM-encoded byte slice.
	//
	// Encoding procedure:
	//   1. Create a PEM block with Type "SIGNATURE".
	//   2. Put the raw signature bytes in the block.
	//   3. Add the signing algorithm (e.g., "RSASSA-PSS") as header "Signature Algorithm".
	//   4. Encode the block to PEM.
	//   5. Append the signer’s certificate chain in PEM format.
	//
	// Verification model:
	//   - The public key may be taken from the appended, validated certificate chain.
	//   - The signature’s logical identity (its OCM signature name) must match the DN of the
	//     trusted certificate used for verification.
	//   - If no external public key is provided, verification MUST use a validated certificate
	//     chain bundled with the signature.
	//   - The signature MUST always be stored together with its certificate chain.
	//
	// See https://github.com/open-component-model/ocm/issues/584 for background.
	//
	// This is the default Policy.
	SignatureEncodingPolicyPEM SignatureEncodingPolicy = "PEM"
)
