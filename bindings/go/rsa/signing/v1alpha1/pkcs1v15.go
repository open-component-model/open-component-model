package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// MediaTypePlainRSASSAPKCS1V15 is the media type for a plain signature based on AlgorithmRSASSAPKCS1V15 encoded as a hex string.
	MediaTypePlainRSASSAPKCS1V15 = "application/vnd.ocm.signature.rsa"

	// AlgorithmRSASSAPKCS1V15 is the identifier for the RSA signature scheme with PKCS #1 v1.5 padding.
	//
	// RSASSA-PKCS1 v1.5 is the legacy RSA signature algorithm, defined in:
	//   - PKCS #1 v1.5 and v2.2: https://datatracker.ietf.org/doc/html/rfc8017#section-8.2
	//   - NIST FIPS 186-4: https://csrc.nist.gov/publications/detail/fips/186/4/final
	//
	// Key properties:
	//   - Based on the RSA cryptosystem with deterministic padding.
	//   - Uses an ASN.1 DigestInfo structure containing the message digest.
	//   - Deterministic: the same message always produces the same signature with the same key.
	//   - Widely implemented and historically the default in many libraries and standards.
	//   - Considered less secure than RSASSA-PSS due to deterministic padding and a history of
	//     padding oracle attacks, but still accepted in many environments for backward compatibility.
	//
	// Verification flow:
	//   1. Perform the RSA public key operation on the signature.
	//   2. Compare the result against the expected ASN.1 DigestInfo structure of the message digest.
	//
	// Parameters used in OCM:
	//   - Hash function: SHA-256, SHA-384, or SHA-512 based on digest specification for the signing handler.
	//   - Padding: fixed PKCS #1 v1.5 encoding (non-probabilistic, non-configurable).
	//
	// Notes:
	//   - RSASSA-PKCS1 v1.5 was the default algorithm in older versions of OCM.
	//   - For new signatures, RSASSA-PSS is recommended and is the default in the new OCM Signature Libraries.
	AlgorithmRSASSAPKCS1V15 = "RSASSA-PKCS1-V1_5"
)

// PKCS1V15Config defines configuration for signing based on RSASSA-PKCS1 v1.5.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type PKCS1V15Config struct {
	// Type identifies this configuration object’s runtime type.
	Type runtime.Type `json:"type"`

	SignatureEncodingPolicy SignatureEncodingPolicy `json:"signatureEncodingPolicy,omitempty"`
}

func (*PKCS1V15Config) MediaType() string {
	return MediaTypePlainRSASSAPKCS1V15
}

func (*PKCS1V15Config) Algorithm() string {
	return AlgorithmRSASSAPKCS1V15
}
