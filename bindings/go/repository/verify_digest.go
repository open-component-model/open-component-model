package repository

import (
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// parseDigest returns d as a [digest.Digest] in canonical "algorithm:hex" form, so that
// content can be verified against it.
//
// Note: There are several places in the code today in which we are parsing digests in
// one way or another. It will be a separate issue to pull them all together. Not in this one.
// And we aren't using those to avoid having to import OCI package or some other package
// and dilute the dependency graph.
func parseDigest(d *descriptor.Digest) (digest.Digest, error) {
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

	algorithm, err := hashAlgorithm(d.HashAlgorithm)
	if err != nil {
		return "", err
	}

	value := strings.ToLower(d.Value)
	// a value spelled "<algorithm>:<hex>", as digest.Digest.String() writes it, would
	// otherwise be prefixed a second time and fail to parse.
	if prefix, encoded, prefixed := strings.Cut(value, ":"); prefixed {
		if prefixAlgorithm, err := hashAlgorithm(prefix); err != nil || prefixAlgorithm != algorithm {
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

// hashAlgorithm maps an OCM hash algorithm name to its digest algorithm. SHA-256 and
// sha256 appear equally, so case and the separator are ignored. Only the algorithms
// OCM accepts for resource digests are known.
func hashAlgorithm(name string) (digest.Algorithm, error) {
	switch strings.ToLower(strings.ReplaceAll(name, "-", "")) {
	case "sha256":
		return digest.SHA256, nil
	case "sha512":
		return digest.SHA512, nil
	default:
		return "", fmt.Errorf("unsupported hash algorithm %q: only SHA-256 and SHA-512 are supported", name)
	}
}
