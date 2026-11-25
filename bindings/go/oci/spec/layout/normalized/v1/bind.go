package v1

import (
	"bytes"
	"errors"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

var errAccessMismatch = errors.New("access descriptor does not match signed normalized descriptor (possible tampering or mismatched referrer)")

// NormalisationAlgorithm is the algorithm used to derive the normalized descriptor layer, and the
// value stored in the AnnotationNormalisationAlgo annotation.
const NormalisationAlgorithm = v4alpha1.Algorithm

// Normalize returns the normalized (access-free, JCS-canonical) descriptor bytes for cd. These
// bytes are the normalized manifest's single layer and the payload cosign's signature covers
// (via the manifest digest).
func Normalize(cd *descruntime.Descriptor) ([]byte, error) {
	return normalisation.Normalise(cd, NormalisationAlgorithm)
}

// VerifyNormalizedMatchesAccess proves that the access-bearing descriptor `access` re-normalizes
// to exactly `normalized` (the access-free bytes the normalized manifest carries). Normalisation
// drops accesses, so this binds an access referrer to the normalized manifest only together with
// ValidateDigests on `access`, and it gives integrity relative to that manifest, not authenticity:
// the manifest itself must be verified (e.g. by a cosign signature or a digest-pinned reference).
func VerifyNormalizedMatchesAccess(normalized []byte, access *descruntime.Descriptor) error {
	recomputed, err := Normalize(access)
	if err != nil {
		return fmt.Errorf("failed to normalize access descriptor: %w", err)
	}
	if !bytes.Equal(recomputed, normalized) {
		return errAccessMismatch
	}
	return nil
}

// VerifyNormalizedLayerMatchesAccess is [VerifyNormalizedMatchesAccess] against the descriptor of
// the normalized layer instead of its content: `access` must re-normalize to bytes of the size and
// digest `layer` records. It needs only the normalized manifest, which lists `layer`, and gives the
// same guarantee as long as that manifest is verified.
func VerifyNormalizedLayerMatchesAccess(layer ociImageSpecV1.Descriptor, access *descruntime.Descriptor) error {
	if err := layer.Digest.Validate(); err != nil {
		return fmt.Errorf("invalid digest %q of normalized descriptor layer: %w", layer.Digest, err)
	}
	recomputed, err := Normalize(access)
	if err != nil {
		return fmt.Errorf("failed to normalize access descriptor: %w", err)
	}
	if int64(len(recomputed)) != layer.Size || layer.Digest.Algorithm().FromBytes(recomputed) != layer.Digest {
		return errAccessMismatch
	}
	return nil
}
