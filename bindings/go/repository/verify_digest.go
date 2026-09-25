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

	// normalize because SHA-256 and sha256 equally appear
	if !strings.EqualFold(strings.ReplaceAll(d.HashAlgorithm, "-", ""), "sha256") {
		return "", fmt.Errorf("unsupported hash algorithm %q: only SHA-256 is supported", d.HashAlgorithm)
	}

	value := strings.ToLower(d.Value)
	// a value spelled "<algorithm>:<hex>", as digest.Digest.String() writes it, would
	// otherwise be prefixed a second time and fail to parse.
	if algorithm, encoded, prefixed := strings.Cut(value, ":"); prefixed {
		if !strings.EqualFold(strings.ReplaceAll(algorithm, "-", ""), "sha256") {
			return "", fmt.Errorf("digest value %q carries algorithm %q but hashAlgorithm is %q", d.Value, algorithm, d.HashAlgorithm)
		}
		value = encoded
	}

	parsed := digest.NewDigestFromEncoded(digest.SHA256, value)
	if err := parsed.Validate(); err != nil {
		return "", fmt.Errorf("invalid digest %q: %w", d.Value, err)
	}

	return parsed, nil
}
