package runtime

import (
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"
)

// Parse returns d as a [digest.Digest] in canonical "algorithm:hex" form, so that
// content can be verified against it.
//
// An empty digest with no error means d declares nothing to verify against, which
// is what --skip-reference-digest-processing produces and is not a failure. An
// error means the digest is present but unusable.
//
// Note: There are several places in the code today in which we are parsing digests in
// one way or another. It will be a separate issue to pull them all together. Not in this one.
// And we aren't using those to avoid having to import OCI package or some other package
// and dilute the dependency graph.
func (d *Digest) Parse() (digest.Digest, error) {
	if d == nil || d.Value == "" || d.HashAlgorithm == "" {
		return "", nil
	}
	if strings.EqualFold(d.HashAlgorithm, NoDigest) || strings.EqualFold(d.NormalisationAlgorithm, ExcludeFromSignature) {
		return "", nil
	}

	// normalize because SHA-256 and sha256 equally appear
	if !strings.EqualFold(strings.ReplaceAll(d.HashAlgorithm, "-", ""), "sha256") {
		return "", fmt.Errorf("unsupported hash algorithm %q: only SHA-256 is supported", d.HashAlgorithm)
	}

	parsed := digest.NewDigestFromEncoded(digest.SHA256, strings.ToLower(d.Value))
	if err := parsed.Validate(); err != nil {
		return "", fmt.Errorf("invalid digest %q: %w", d.Value, err)
	}

	return parsed, nil
}
