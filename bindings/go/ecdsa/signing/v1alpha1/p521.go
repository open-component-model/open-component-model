package v1alpha1

const (
	// MediaTypePlainECDSAP521 is the media type for a plain signature based on AlgorithmECDSAP521
	// encoded as a hex string of ASN.1 DER bytes.
	MediaTypePlainECDSAP521 = "application/vnd.ocm.signature.ecdsa.p521"

	// AlgorithmECDSAP521 is the identifier for the Elliptic Curve Digital Signature Algorithm
	// using the NIST P-521 curve (also known as secp521r1).
	//
	// ECDSA P-521 is defined in:
	//   - FIPS 186-5: https://csrc.nist.gov/publications/detail/fips/186/5/final
	//   - SEC 2: https://www.secg.org/sec2-v2.pdf
	//
	// Key properties:
	//   - Provides approximately 256-bit security, the highest of the NIST curves.
	//   - Produces ASN.1 DER signatures of approximately 137-139 bytes.
	//   - Used in high-security environments where maximum curve strength is required.
	//
	// Parameters used in OCM:
	//   - Hash function: SHA-256, SHA-384, or SHA-512 based on digest specification.
	//   - Recommended hash: SHA-512 (matching curve security level).
	AlgorithmECDSAP521 SignatureAlgorithm = "ECDSA-P521"
)
