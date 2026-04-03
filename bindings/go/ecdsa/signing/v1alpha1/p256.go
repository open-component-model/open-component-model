package v1alpha1

const (
	// MediaTypePlainECDSAP256 is the media type for a plain signature based on AlgorithmECDSAP256
	// encoded as a hex string of ASN.1 DER bytes.
	MediaTypePlainECDSAP256 = "application/vnd.ocm.signature.ecdsa.p256"

	// AlgorithmECDSAP256 is the identifier for the Elliptic Curve Digital Signature Algorithm
	// using the NIST P-256 curve (also known as secp256r1 or prime256v1).
	//
	// ECDSA P-256 is defined in:
	//   - FIPS 186-5: https://csrc.nist.gov/publications/detail/fips/186/5/final
	//   - SEC 2: https://www.secg.org/sec2-v2.pdf
	//   - RFC 6979 (deterministic variant): https://datatracker.ietf.org/doc/html/rfc6979
	//
	// Key properties:
	//   - Provides approximately 128-bit security, equivalent to RSA-3072.
	//   - Produces compact ASN.1 DER signatures of approximately 70-72 bytes.
	//   - Non-deterministic by default (uses random nonce via crypto/rand).
	//   - Widely adopted as the default algorithm in Sigstore, cloud KMS services,
	//     and OCI artifact signing (Notation).
	//
	// Verification flow:
	//   1. Parse the ASN.1 DER encoded (r, s) integer pair from the signature.
	//   2. Perform the ECDSA verification operation using the public key and hash.
	//
	// Parameters used in OCM:
	//   - Hash function: SHA-256, SHA-384, or SHA-512 based on digest specification.
	//   - Recommended hash: SHA-256 (matching curve security level).
	//
	// This is the default algorithm for ECDSA signing in OCM.
	AlgorithmECDSAP256 SignatureAlgorithm = "ECDSA-P256"
)
