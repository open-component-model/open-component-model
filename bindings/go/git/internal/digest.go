package internal

import (
	"fmt"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	HashAlgorithm          = "SHA-256"
	NormalisationAlgorithm = "genericBlobDigest/v1"
)

func VerifyDigest(expected *descriptor.Digest, value string) error {
	if expected == nil {
		return nil
	}

	if expected.HashAlgorithm != "" && !strings.EqualFold(expected.HashAlgorithm, HashAlgorithm) {
		return fmt.Errorf("unsupported git hash algorithm %q", expected.HashAlgorithm)
	}

	if expected.NormalisationAlgorithm != "" && !strings.EqualFold(expected.NormalisationAlgorithm, NormalisationAlgorithm) {
		return fmt.Errorf("unsupported git normalisation algorithm %q", expected.NormalisationAlgorithm)
	}

	if !strings.EqualFold(expected.Value, value) {
		return fmt.Errorf("git archive digest mismatch: expected %s, got %s", expected.Value, value)
	}

	return nil
}
