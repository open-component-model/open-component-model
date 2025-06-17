package digest

import (
	"fmt"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	HashAlgorithmSHA256 = "SHA-256"
)

var SHAMapping = map[string]digest.Algorithm{
	HashAlgorithmSHA256: digest.SHA256,
}

var ReverseSHAMapping = reverseMap(SHAMapping)

// Apply applies the given digest to the target digest structure.
// It sets the Digest field of the resource to a new Digest object
// with the specified hash algorithm and normalisation algorithm.
// The Mappings are defined by OCM and are static.
// They mainly differ in the algorithm name, but are semantically equivalent.
func Apply(target *runtime.Digest, digest digest.Digest) error {
	algo, ok := ReverseSHAMapping[digest.Algorithm()]
	if !ok {
		return fmt.Errorf("unknown algorithm: %s", digest.Algorithm())
	}
	target.HashAlgorithm = algo
	target.NormalisationAlgorithm = "genericBlobDigest/v1" // TODO use a constant from blob package for this
	target.Value = digest.Encoded()

	return nil
}

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
	if target.HashAlgorithm != ReverseSHAMapping[digest.Algorithm()] {
		return fmt.Errorf("hash algorithm mismatch: expected %s, got %s", target.HashAlgorithm, ReverseSHAMapping[digest.Algorithm()])
	}
	return nil
}

func reverseMap[K, V comparable](m map[K]V) map[V]K {
	reversed := make(map[V]K)
	for k, v := range m {
		reversed[v] = k
	}
	return reversed
}
