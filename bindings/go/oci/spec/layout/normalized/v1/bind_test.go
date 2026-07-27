package v1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	layout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
)

func bindTestDescriptor() *descruntime.Descriptor {
	d := &descruntime.Descriptor{}
	d.Component.Name = "example.org/comp"
	d.Component.Version = "1.0.0"
	d.Component.Provider = descruntime.Provider{Name: "internal"}
	return d
}

func TestNormalize_MatchesNormalisationPackage(t *testing.T) {
	d := bindTestDescriptor()
	want, err := normalisation.Normalise(d, v4alpha1.Algorithm)
	require.NoError(t, err)
	got, err := layout.Normalize(d)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestVerifyNormalizedMatchesAccess_OK(t *testing.T) {
	f := bindTestDescriptor()
	n, err := layout.Normalize(f)
	require.NoError(t, err)
	require.NoError(t, layout.VerifyNormalizedMatchesAccess(n, f))
}

func TestVerifyNormalizedMatchesAccess_Mismatch(t *testing.T) {
	f := bindTestDescriptor()
	n, err := layout.Normalize(f)
	require.NoError(t, err)

	tampered := bindTestDescriptor()
	tampered.Component.Name = "example.org/evil"

	err = layout.VerifyNormalizedMatchesAccess(n, tampered)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}
