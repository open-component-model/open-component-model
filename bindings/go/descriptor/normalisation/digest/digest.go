package digest

import (
	"crypto"
	"fmt"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

var ReverseSHAMapping = map[digest.Algorithm]string{
	digest.SHA256: crypto.SHA256.String(),
	digest.SHA512: crypto.SHA512.String(),
}

// DigestNormalizedDescriptor normalises and digests the given descriptor.
func DigestNormalizedDescriptor(desc *runtime.Descriptor, hashAlgo digest.Algorithm, normalisationAlgo normalisation.Algorithm) (*runtime.Digest, error) {
	normalisedData, err := normalisation.Normalisations.Normalise(desc, normalisationAlgo)
	if err != nil {
		return nil, fmt.Errorf("error normalising descriptor %s: %w", desc.Component.ToIdentity().String(), err)
	}
	algo, ok := ReverseSHAMapping[hashAlgo]
	if !ok {
		return nil, fmt.Errorf("unsupported hash algorithm: %s", hashAlgo)
	}

	descriptorDigest := &runtime.Digest{
		HashAlgorithm:          algo,
		NormalisationAlgorithm: normalisationAlgo,
		Value:                  hashAlgo.FromBytes(normalisedData).Encoded(),
	}

	return descriptorDigest, nil
}
