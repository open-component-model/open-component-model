package v1

import (
	"bytes"
	"fmt"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

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
		return fmt.Errorf("access descriptor does not match signed normalized descriptor (possible tampering or mismatched referrer)")
	}
	return nil
}
