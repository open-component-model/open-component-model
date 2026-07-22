package v1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	layout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
)

func descriptorWithResourceDigest(d *descruntime.Digest) *descruntime.Descriptor {
	desc := &descruntime.Descriptor{}
	desc.Component.Name = "example.org/comp"
	desc.Component.Version = "1.0.0"
	r := descruntime.Resource{Type: "ociImage"}
	r.Name = "img"
	r.Version = "1.0.0"
	r.Digest = d
	desc.Component.Resources = []descruntime.Resource{r}
	return desc
}

func TestRequireAllResourcesDigested_OK(t *testing.T) {
	desc := descriptorWithResourceDigest(&descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  "abc",
	})
	require.NoError(t, layout.RequireAllResourcesDigested(desc))
}

func TestRequireAllResourcesDigested_MissingDigest(t *testing.T) {
	desc := descriptorWithResourceDigest(nil)
	err := layout.RequireAllResourcesDigested(desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "img")
}

func TestRequireAllResourcesDigested_WeakHash(t *testing.T) {
	desc := descriptorWithResourceDigest(&descruntime.Digest{HashAlgorithm: "SHA-1", Value: "abc"})
	err := layout.RequireAllResourcesDigested(desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SHA-1")
}

func TestRequireAllResourcesDigested_ExcludedSentinelAllowed(t *testing.T) {
	desc := descriptorWithResourceDigest(&descruntime.Digest{
		HashAlgorithm:          descruntime.NoDigest,
		NormalisationAlgorithm: descruntime.ExcludeFromSignature,
	})
	require.NoError(t, layout.RequireAllResourcesDigested(desc))
}
