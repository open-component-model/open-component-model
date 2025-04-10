package v1

import (
	"fmt"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	OCIArtifactDigestAlgorithmType    = "ociArtifactDigest"
	OCIArtifactDigestAlgorithmVersion = "v1"
	OCIArtifactDigestAlgorithm        = OCIArtifactDigestAlgorithmType + "/" + OCIArtifactDigestAlgorithmVersion
)

const (
	HashAlgorithmSHA256 = "SHA-256"
)

var SHAMapping = map[string]digest.Algorithm{
	HashAlgorithmSHA256: digest.SHA256,
}

var ReverseSHAMapping = reverseMap(SHAMapping)

// ApplyToResource applies the given digest to the resource.
// It sets the Digest field of the resource to a new Digest object
// with the specified hash algorithm and normalisation algorithm.
// The Mappings are defined by OCM and are static.
// They mainly differ in the algorithm name, but are semantically equivalent.
func ApplyToResource(resource *runtime.Resource, digest digest.Digest, algorithm string) error {
	if resource == nil {
		return fmt.Errorf("resource must not be nil")
	}
	algo, ok := ReverseSHAMapping[digest.Algorithm()]
	if !ok {
		return fmt.Errorf("unknown algorithm: %s", digest.Algorithm())
	}
	resource.Digest = &runtime.Digest{
		HashAlgorithm:          algo,
		NormalisationAlgorithm: algorithm,
		Value:                  digest.Encoded(),
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
