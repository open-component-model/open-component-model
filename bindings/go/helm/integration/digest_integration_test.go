// Package integration exercises the helm bindings against public chart repositories.
package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/digest"
	v1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_Integration_ProcessResourceDigest_HTTPRepo(t *testing.T) {
	r := require.New(t)
	t.Parallel()

	resource := helmAccessResource(t, "https://grafana.github.io/helm-charts", "grafana:8.8.2")
	result, err := digest.NewDigestProcessor(t.TempDir()).ProcessResourceDigest(t.Context(), resource, nil)
	r.NoError(err)
	r.NotNil(result)
	r.NotNil(result.Digest)
	r.Equal("SHA-256", result.Digest.HashAlgorithm)
	r.Equal("genericBlobDigest/v1", result.Digest.NormalisationAlgorithm)
	r.Equal("b244a04ccfac950853080eede6bf1b716b2966ca707e69f6f3623b3d690e36b8", result.Digest.Value)
}

func Test_Integration_ProcessResourceDigest_OCIRepo(t *testing.T) {
	r := require.New(t)
	t.Parallel()

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

func helmAccessResource(t *testing.T, helmRepository, helmChart string) *descriptor.Resource {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"type":           "helm/v1",
		"helmRepository": helmRepository,
		"helmChart":      helmChart,
	})
	require.NoError(t, err)

	r := &descriptor.Resource{}
	r.Name = "mychart"
	r.Version = "1.0.0"
	r.Type = "helmChart"
	r.Access = &runtime.Raw{
		Type: runtime.NewVersionedType(v1.LegacyType, v1.LegacyTypeVersion),
		Data: raw,
	}
	return r
}
