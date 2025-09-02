package hashing

import (
	"fmt"

	"github.com/opencontainers/go-digest"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	HashAlgorithmSHA256Legacy = "SHA-256"
	HashAlgorithmSHA512Legacy = "SHA-512"
)

var SHAMapping = map[string]digest.Algorithm{
	HashAlgorithmSHA256Legacy: digest.SHA256,
	HashAlgorithmSHA512Legacy: digest.SHA512,
	digest.SHA256.String():    digest.SHA256,
	digest.SHA512.String():    digest.SHA512,
}

var ReverseSHAMapping = map[digest.Algorithm]string{
	digest.SHA256: HashAlgorithmSHA256Legacy,
	digest.SHA512: HashAlgorithmSHA512Legacy,
}

// DigestNormalizedDescriptor normalises and digests the given descriptor.
func DigestNormalizedDescriptor(desc *runtime.Descriptor, hashAlgo digest.Algorithm, normalisationAlgo normalisation.Algorithm) (*runtime.Digest, error) {
	normalisedData, err := normalisation.Normalisations.Normalise(desc, normalisationAlgo)
	if err != nil {
		return nil, fmt.Errorf("error normalising descriptor %s: %w", desc.Component.ToIdentity().String(), err)
	}
	descriptorDigest := &runtime.Digest{
		HashAlgorithm:          ReverseSHAMapping[hashAlgo],
		NormalisationAlgorithm: normalisationAlgo,
		Value:                  hashAlgo.FromBytes(normalisedData).Encoded(),
	}

	return descriptorDigest, nil
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
	algo, ok := ReverseSHAMapping[digest.Algorithm()]
	if !ok {
		return fmt.Errorf("unknown algorithm in digest: %s", digest.Algorithm())
	}
	if target.HashAlgorithm != algo {
		return fmt.Errorf("hash algorithm mismatch: expected %s, got %s", target.HashAlgorithm, ReverseSHAMapping[digest.Algorithm()])
	}
	return nil
}
