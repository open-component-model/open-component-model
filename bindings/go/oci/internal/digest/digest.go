package digest

import (
	"fmt"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	HashAlgorithmSHA256 = "SHA-256"
	GenericBlobDigestV1 = "genericBlobDigest/v1"
	OCIArtifactDigestV1 = "ociArtifactDigest/v1"
)

var SHAMapping = map[string]digest.Algorithm{
	HashAlgorithmSHA256: digest.SHA256,
}

var ReverseSHAMapping = reverseMap(SHAMapping)

// Apply records a digest with the normalization of the content that was hashed.
func Apply(target *runtime.Digest, digest digest.Digest, normalisation string) error {
	algo, ok := ReverseSHAMapping[digest.Algorithm()]
	if !ok {
		return fmt.Errorf("unknown algorithm: %s", digest.Algorithm())
	}
	if normalisation != GenericBlobDigestV1 && normalisation != OCIArtifactDigestV1 {
		return fmt.Errorf("unsupported normalisation algorithm: %s", normalisation)
	}
	target.HashAlgorithm = algo
	target.NormalisationAlgorithm = normalisation
	target.Value = digest.Encoded()

	return nil
}

// Verify checks a digest without treating different normalizations as interchangeable.
func Verify(target *runtime.Digest, digest digest.Digest, normalisation string) error {
	if target == nil {
		return fmt.Errorf("target digest is nil")
	}
	if target.NormalisationAlgorithm != normalisation {
		return fmt.Errorf("normalisation algorithm mismatch: expected %s, got %s", normalisation, target.NormalisationAlgorithm)
	}
	return verifyHash(target, digest)
}

// VerifyOCIArtifact accepts the historical v2 generic label only at an OCI artifact
// boundary, where digest is the resolved manifest/index hash, never a blob checksum.
// Keeping the original triple avoids invalidating signatures on published descriptors.
func VerifyOCIArtifact(target *runtime.Digest, digest digest.Digest) error {
	if target == nil {
		return fmt.Errorf("target digest is nil")
	}
	switch target.NormalisationAlgorithm {
	case OCIArtifactDigestV1, GenericBlobDigestV1:
		return verifyHash(target, digest)
	default:
		return fmt.Errorf("unsupported OCI artifact normalisation algorithm: %s", target.NormalisationAlgorithm)
	}
}

func verifyHash(target *runtime.Digest, digest digest.Digest) error {
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

func reverseMap[K, V comparable](m map[K]V) map[V]K {
	reversed := make(map[V]K)
	for k, v := range m {
		reversed[v] = k
	}
	return reversed
}
