// Package digest provides the OCI rules for checking resource digests with the shared
// [ocmdigest.Check]: which normalisation describes an OCI artifact or a plain blob, and how
// an OCI digest maps to a computed digest.
package digest

import (
	"crypto"
	"fmt"

	godigest "github.com/opencontainers/go-digest"

	ocmdigest "ocm.software/open-component-model/bindings/go/digest"
)

const (
	// ArtifactDigestV1 is the normalisation of a digest over the manifest or index of an OCI artifact.
	ArtifactDigestV1 = "ociArtifactDigest/v1"
	// LegacyArtifactDigestV1 is the spelling OCM v1 also registers for ArtifactDigestV1.
	LegacyArtifactDigestV1 = "ociArtefactDigest/v1"
)

var (
	// ArtifactRule applies when the digest is the root manifest or index of an OCI artifact.
	// Earlier v2 releases labelled OCI artifacts with the generic blob normalisation, and those
	// digests are signed, so that label stays accepted, verified against the manifest digest.
	ArtifactRule = ocmdigest.Rule{
		Normalisation: ArtifactDigestV1,
		Accepted:      []string{LegacyArtifactDigestV1, ocmdigest.GenericBlobDigestV1},
	}
	// BlobRule applies when the digest is the hash of plain bytes.
	BlobRule = ocmdigest.Rule{Normalisation: ocmdigest.GenericBlobDigestV1}
)

// FromOCI converts an OCI digest to a computed digest.
func FromOCI(d godigest.Digest) (ocmdigest.Computed, error) {
	if err := d.Validate(); err != nil {
		return ocmdigest.Computed{}, fmt.Errorf("invalid OCI digest %q: %w", d, err)
	}
	var hash crypto.Hash
	switch d.Algorithm() {
	case godigest.SHA256:
		hash = crypto.SHA256
	case godigest.SHA512:
		hash = crypto.SHA512
	default:
		return ocmdigest.Computed{}, fmt.Errorf("%w: unknown algorithm %s", ocmdigest.ErrUnsupportedHashAlgorithm, d.Algorithm())
	}
	return ocmdigest.Computed{Hash: hash, Value: d.Encoded()}, nil
}
