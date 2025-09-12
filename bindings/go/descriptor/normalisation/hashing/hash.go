package hashing

import (
	"fmt"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

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
