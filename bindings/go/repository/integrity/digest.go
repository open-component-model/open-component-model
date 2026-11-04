package integrity

import (
	"crypto"
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// ParseDigest returns d as a [digest.Digest] in canonical "algorithm:hex" form, so that
// content can be verified against it.
func ParseDigest(d *descriptor.Digest) (digest.Digest, error) {
	if d == nil {
		return "", nil
	}
	if strings.EqualFold(d.HashAlgorithm, descriptor.NoDigest) || strings.EqualFold(d.NormalisationAlgorithm, descriptor.ExcludeFromSignature) {
		return "", nil
	}
	if d.Value == "" && d.HashAlgorithm == "" {
		return "", nil
	}
	if d.Value == "" || d.HashAlgorithm == "" {
		return "", fmt.Errorf("incomplete digest: hashAlgorithm=%q, value=%q", d.HashAlgorithm, d.Value)
	}

	algorithm, err := HashAlgorithm(d.HashAlgorithm)
	if err != nil {
		return "", err
	}

	value := strings.ToLower(d.Value)
	// a value spelled "<algorithm>:<hex>", as digest.Digest.String() writes it, would
	// otherwise be prefixed a second time and fail to parse.
	if prefix, encoded, prefixed := strings.Cut(value, ":"); prefixed {
		if prefixAlgorithm, err := HashAlgorithm(prefix); err != nil || prefixAlgorithm != algorithm {
			return "", fmt.Errorf("digest value %q carries algorithm %q but hashAlgorithm is %q", d.Value, prefix, d.HashAlgorithm)
		}
		value = encoded
	}

	parsed := digest.NewDigestFromEncoded(algorithm, value)
	if err := parsed.Validate(); err != nil {
		return "", fmt.Errorf("invalid digest %q: %w", d.Value, err)
	}

	return parsed, nil
}

// HashAlgorithm maps a hash algorithm name ("SHA-256" as the spec spells it, or "sha256")
// to the digest algorithm content can be verified with: SHA-256 or SHA-512, the hashes
// signing accepts (see algorithms.Hash).
func HashAlgorithm(name string) (digest.Algorithm, error) {
	hash, err := algorithms.Hash(name)
	if err != nil {
		return "", err
	}
	if hash == crypto.SHA512 {
		return digest.SHA512, nil
	}
	return digest.SHA256, nil
}
