package digest

import (
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	HashAlgorithmSHA256 = "SHA-256"
	HashAlgorithmSHA512 = "SHA-512"
	GenericBlobDigestV1 = "genericBlobDigest/v1"
	OCIArtifactDigestV1 = "ociArtifactDigest/v1"
)

var SHAMapping = map[string]digest.Algorithm{
	HashAlgorithmSHA256: digest.SHA256,
	HashAlgorithmSHA512: digest.SHA512,
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

// IsComplete reports whether hash algorithm, normalisation algorithm, and value are all set.
func IsComplete(target *runtime.Digest) bool {
	return target != nil && target.HashAlgorithm != "" && target.NormalisationAlgorithm != "" && target.Value != ""
}

// IsEmpty reports whether target carries no digest at all: it is nil or every field is unset.
// A resource digest is valid only when it is empty (the resource is omitted from the
// signature) or complete (see IsComplete); a partially specified digest is rejected by
// callers rather than inferred.
func IsEmpty(target *runtime.Digest) bool {
	return target == nil || (target.HashAlgorithm == "" && target.NormalisationAlgorithm == "" && target.Value == "")
}

// sameHashAlgorithm accepts both the OCM ("SHA-256") and OCI ("sha256") spellings,
// since incomplete metadata is often copied from an OCI descriptor.
func sameHashAlgorithm(a, b string) bool {
	return strings.EqualFold(strings.ReplaceAll(a, "-", ""), strings.ReplaceAll(b, "-", ""))
}

func verifyHash(target *runtime.Digest, digest digest.Digest) error {
	if target.Value != digest.Encoded() {
		return fmt.Errorf("digest value mismatch: expected %s, got %s", target.Value, digest.Encoded())
	}
	algo, ok := ReverseSHAMapping[digest.Algorithm()]
	if !ok {
		return fmt.Errorf("unknown algorithm in digest: %s", digest.Algorithm())
	}
	if !sameHashAlgorithm(target.HashAlgorithm, algo) {
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
