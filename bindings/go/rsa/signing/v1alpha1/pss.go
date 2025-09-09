package v1alpha1

const (
	// MediaTypePlainRSASSAPSS is the media type for a plain signature based on AlgorithmRSASSAPSS encoded as a hex string.
	MediaTypePlainRSASSAPSS = "application/vnd.ocm.signature.rsa.pss"

	// AlgorithmRSASSAPSS is the identifier for the RSA Probabilistic Signature Scheme (RSASSA-PSS).
	//
	// RSASSA-PSS is the recommended modern RSA signature algorithm, defined in:
	//   - PKCS #1 v2.1: https://datatracker.ietf.org/doc/html/rfc3447#section-8.1
	//   - NIST FIPS 186-4: https://csrc.nist.gov/publications/detail/fips/186/4/final
	//
	// Key properties:
	//   - Based on the RSA cryptosystem with probabilistic padding.
	//   - Uses a random salt and a mask generation function (MGF1, usually SHA-2).
	//   - Non-deterministic: the same message produces different signatures when signed multiple times.
	//   - Stronger theoretical security guarantees than the older deterministic RSASSA-PKCS1 v1.5 scheme.
	//
	// Verification flow:
	//   1. Apply the same padding function (with expected salt length and hash).
	//   2. Perform the RSA public key operation on the signature.
	//   3. Compare the result against the expected padded hash value.
	//
	// Parameters used in OCM:
	//   - Hash function: SHA-256, SHA-384, or SHA-512 based on digest specification for the signing handler.
	//   - Salt length: equal to the length of the hash output.
	//   - MGF: MGF1 with the same underlying hash function, non modifiable.
	//
	// This is the default algorithm for the new OCM Signature Libraries, but older version of OCM used
	// RSASSA-PKCS1 v1.5 as default.
	AlgorithmRSASSAPSS = "RSASSA-PSS"
)
