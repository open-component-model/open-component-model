package digest

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// ErrUnsupportedHashAlgorithm is returned for a hash algorithm other than SHA-256 or SHA-512.
// It is the error of the shared hash-name parser [algorithms.Hash], re-exported so that a
// binding can match it without depending on the descriptor normalisation.
var ErrUnsupportedHashAlgorithm = algorithms.ErrUnsupportedHash

// IsExcluded reports whether a digest is the marker of a resource that is excluded from the
// signature. Either field marks it, matched the same way signing does.
func IsExcluded(d *descriptor.Digest) bool {
	return d != nil && (strings.EqualFold(d.HashAlgorithm, descriptor.NoDigest) ||
		strings.EqualFold(d.NormalisationAlgorithm, descriptor.ExcludeFromSignature))
}
