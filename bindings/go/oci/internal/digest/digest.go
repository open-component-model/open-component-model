package digest

import (
	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const (
	HashAlgorithmSHA256 = "SHA-256"
	HashAlgorithmSHA512 = "SHA-512"
	GenericBlobDigestV1 = runtime.GenericBlobDigestV1
	OCIArtifactDigestV1 = runtime.OCIArtifactDigestV1
)

var SHAMapping = map[string]digest.Algorithm{
	HashAlgorithmSHA256: digest.SHA256,
	HashAlgorithmSHA512: digest.SHA512,
}

var ReverseSHAMapping = reverseMap(SHAMapping)

func reverseMap[K, V comparable](m map[K]V) map[V]K {
	reversed := make(map[V]K)
	for k, v := range m {
		reversed[v] = k
	}
	return reversed
}
