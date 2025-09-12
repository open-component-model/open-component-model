package hashing

import (
	"fmt"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Verify checks if the target digest matches the provided digest.
// It compares the Value and HashAlgorithm fields of the target
// with the encoded value and algorithm of the provided digest.
func Verify(target *runtime.Digest, digest digest.Digest) error {
	if target == nil {
		return fmt.Errorf("target digest is nil")
	}
	if target.Value != digest.Encoded() {
		return fmt.Errorf("digest value mismatch: expected %s, got %s", target.Value, digest.Encoded())
	}
	algo, ok := ReverseSHAMapping[digest.Algorithm()]
	if !ok {
		return fmt.Errorf("unknown algorithm in digest: %s", digest.Algorithm())
	}
	if target.HashAlgorithm != algo {
		return fmt.Errorf("hash algorithm mismatch: expected %s, got %s", target.HashAlgorithm, ReverseSHAMapping[digest.Algorithm()])
	}
	return nil
}
