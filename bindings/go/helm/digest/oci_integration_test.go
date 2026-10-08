package digest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/digest"
)

func TestProcessResourceDigest_RealOCIRepo(t *testing.T) {
	r := require.New(t)
	if testing.Short() {
		t.Skip("skipping real OCI registry test in short mode")
	}
	resource := helmAccessResource(t, "oci://ghcr.io/stefanprodan/charts", "podinfo:6.9.1")
	processed, err := digest.NewDigestProcessor("").ProcessResourceDigest(t.Context(), resource, nil)
	r.NoError(err)
	r.Equal("SHA-256", processed.Digest.HashAlgorithm)
	r.Equal("ociArtifactDigest/v1", processed.Digest.NormalisationAlgorithm)
	r.NotEmpty(processed.Digest.Value)

	resource.Digest = &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  processed.Digest.Value,
	}
	legacy, err := digest.NewDigestProcessor("").ProcessResourceDigest(t.Context(), resource, nil)
	r.NoError(err)
	r.Equal(resource.Digest, legacy.Digest)
}
