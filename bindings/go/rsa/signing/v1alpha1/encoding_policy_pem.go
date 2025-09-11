package v1alpha1

const (
	// MediaTypePEM is the media type for a PEM-encoded RSA signature.
	// It represents a signature encoded via SignatureEncodingPolicyPEM.
	MediaTypePEM = "application/x-pem-file"

	// SignatureEncodingPolicyPEM encodes the signature in a PEM block, optionally
	// followed by the signer’s certificate chain.
	//
	// Encoding procedure:
	//   1. Create a PEM block with type "SIGNATURE".
	//   2. Insert the raw signature bytes into the block.
	//   3. Add the signing algorithm (e.g. "RSASSA-PSS") as the "Signature Algorithm" header.
	//   4. Encode the block into PEM format.
	//   5. Optionally append the signer’s certificate chain in PEM format
	//      (only possible if the signature was created with a certificate).
	//
	// Verification rules:
	//   1. The public key may be extracted from an appended and validated certificate chain.
	//   2. The signature’s logical identity (its OCM signature name) must match the
	//      Distinguished Name (DN) of the trusted certificate used for verification.
	//   3. If no external public key is supplied, verification MUST use a validated
	//      certificate chain bundled with the signature. Validation can rely on the host
	//      system’s trust store or on a distributed PKI root certificate.
	//   4. Every signature MUST be stored together with its certificate chain to ensure
	//      that the trust path can be verified upon retrieval.
	//
	// Notes:
	//   - This is the default signature encoding policy.
	//   - Background: https://github.com/open-component-model/ocm/issues/584
	//
	// Experimental: This encoding policy is experimental and may change or be deprecated in the future.
	SignatureEncodingPolicyPEM SignatureEncodingPolicy = "PEM"
)
