package v1alpha1

const (
	// MediaTypePlainECDSAP384 is the media type for a plain signature based on AlgorithmECDSAP384
	// encoded as a hex string of ASN.1 DER bytes.
	MediaTypePlainECDSAP384 = "application/vnd.ocm.signature.ecdsa.p384"

	// AlgorithmECDSAP384 is the identifier for the Elliptic Curve Digital Signature Algorithm
	// using the NIST P-384 curve (also known as secp384r1).
	//
	// ECDSA P-384 is defined in:
	//   - FIPS 186-5: https://csrc.nist.gov/publications/detail/fips/186/5/final
	//   - SEC 2: https://www.secg.org/sec2-v2.pdf
	//
	// Key properties:
	//   - Provides approximately 192-bit security, equivalent to RSA-7680.
	//   - Produces ASN.1 DER signatures of approximately 102-104 bytes.
	//   - Commonly required in FIPS-certified and government environments.
	//
	// Parameters used in OCM:
	//   - Hash function: SHA-256, SHA-384, or SHA-512 based on digest specification.
	//   - Recommended hash: SHA-384 (matching curve security level).
	AlgorithmECDSAP384 SignatureAlgorithm = "ECDSA-P384"
)
