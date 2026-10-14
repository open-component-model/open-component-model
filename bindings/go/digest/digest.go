package digest

import (
	"crypto"
	"errors"
	"fmt"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	HashAlgorithmSHA256 = "SHA-256"
	HashAlgorithmSHA512 = "SHA-512"
	// GenericBlobDigestV1 is the normalisation of a digest over the bytes of a blob.
	GenericBlobDigestV1 = "genericBlobDigest/v1"
)

var (
	// ErrNoDigest is returned when a hash algorithm is the NO-DIGEST marker of a resource
	// that is excluded from the signature.
	ErrNoDigest = errors.New("resource is excluded from the signature and has no digest")
	// ErrUnsupportedHashAlgorithm is returned for a hash algorithm other than SHA-256 or SHA-512.
	ErrUnsupportedHashAlgorithm = errors.New("unsupported hash algorithm")
)

// ParseHashAlgorithm returns the hash for a recorded hash algorithm name. Recorded digests
// spell the names differently ("SHA-256", "sha256", case variants), so case and dashes are
// ignored.
func ParseHashAlgorithm(name string) (crypto.Hash, error) {
	switch strings.ToUpper(strings.ReplaceAll(name, "-", "")) {
	case "SHA256":
		return crypto.SHA256, nil
	case "SHA512":
		return crypto.SHA512, nil
	case "NODIGEST":
		return 0, ErrNoDigest
	default:
		return 0, fmt.Errorf("%w %q: use %s or %s", ErrUnsupportedHashAlgorithm, name, HashAlgorithmSHA256, HashAlgorithmSHA512)
	}
}

// FormatHashAlgorithm returns the spec spelling of a hash, used for new and completed digests.
func FormatHashAlgorithm(hash crypto.Hash) (string, error) {
	switch hash {
	case crypto.SHA256:
		return HashAlgorithmSHA256, nil
	case crypto.SHA512:
		return HashAlgorithmSHA512, nil
	default:
		return "", fmt.Errorf("%w %q: use %s or %s", ErrUnsupportedHashAlgorithm, hash, HashAlgorithmSHA256, HashAlgorithmSHA512)
	}
}

// IsExcluded reports whether a digest is the marker of a resource that is excluded from the
// signature. Either field marks it, matched the same way signing does.
func IsExcluded(d *descriptor.Digest) bool {
	return d != nil && (strings.EqualFold(d.HashAlgorithm, descriptor.NoDigest) ||
		strings.EqualFold(d.NormalisationAlgorithm, descriptor.ExcludeFromSignature))
}
