package v1

import (
	"fmt"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const OCIArtifactDigestAlgorithm = "ociArtifactDigest"
const OCIArtifactDigestAlgorithmVersion = "v1"

var SHAMapping = map[string]digest.Algorithm{
	"SHA-256": digest.SHA256,
}

var ReverseSHAMapping = map[digest.Algorithm]string{
	digest.SHA256: "SHA-256",
}

func ApplyToResource(resource *runtime.Resource, digest digest.Digest) error {
	if resource == nil {
		return fmt.Errorf("resource must not be nil")
	}
	algo, ok := ReverseSHAMapping[digest.Algorithm()]
	if !ok {
		return fmt.Errorf("unknown algorithm: %s", digest.Algorithm())
	}
	resource.Digest = &runtime.Digest{
		HashAlgorithm:          algo,
		NormalisationAlgorithm: fmt.Sprintf("%s/%s", OCIArtifactDigestAlgorithm, OCIArtifactDigestAlgorithmVersion),
		Value:                  digest.Encoded(),
	}

	return nil
}
