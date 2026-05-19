package v1alpha1

const (
	// AlgorithmGPG is the identifier for OpenPGP detached signatures.
	AlgorithmGPG = "GPG"

	// MediaTypeGPG is the media type for an ASCII-armored OpenPGP detached signature.
	MediaTypeGPG = "application/vnd.ocm.signature.gpg"
)

// HashAlgorithm names the hash function used when signing digest bytes.
type HashAlgorithm string

const (
	HashAlgorithmSHA256 HashAlgorithm = "SHA-256"
	HashAlgorithmSHA384 HashAlgorithm = "SHA-384"
	HashAlgorithmSHA512 HashAlgorithm = "SHA-512"
)
