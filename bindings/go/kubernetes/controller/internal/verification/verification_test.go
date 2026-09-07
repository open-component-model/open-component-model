package verification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
)

func config(t *testing.T, content string) *configuration.Configuration {
	t.Helper()

	var cfg genericv1.Config
	require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(content), &cfg))

	return &configuration.Configuration{Config: &cfg}
}

func TestGetVerifications(t *testing.T) {
	t.Run("nil configuration requests nothing", func(t *testing.T) {
		verifications, err := GetVerifications(nil)
		require.NoError(t, err)
		assert.Empty(t, verifications)
	})

	t.Run("config without signing entries requests nothing", func(t *testing.T) {
		verifications, err := GetVerifications(config(t, `
type: generic.config.ocm.software/v1
configurations:
- type: credentials.config.ocm.software
  consumers: []
`))
		require.NoError(t, err)
		assert.Empty(t, verifications)
	})

	t.Run("entry without a signature name requests nothing", func(t *testing.T) {
		verifications, err := GetVerifications(config(t, `
type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  verifier:
    type: RSASigningConfiguration/v1alpha1
`))
		require.NoError(t, err)
		assert.Empty(t, verifications)
	})

	t.Run("named entry without a verifier defaults to RSASSA-PSS", func(t *testing.T) {
		verifications, err := GetVerifications(config(t, `
type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  signature: release
`))
		require.NoError(t, err)
		require.Len(t, verifications, 1)
		assert.Equal(t, "release", verifications[0].Signature)
		assert.Equal(t, "RSASigningConfiguration", verifications[0].Verifier.GetType().String())
	})

	t.Run("only named signatures are requested", func(t *testing.T) {
		verifications, err := GetVerifications(config(t, `
type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  signature: release
- type: signing.config.ocm.software/v1alpha1
  signature: nightly
`))
		require.NoError(t, err)
		require.Len(t, verifications, 2)
		assert.Equal(t, "release", verifications[0].Signature)
		assert.Equal(t, "nightly", verifications[1].Signature)
	})

	t.Run("a later entry for the same signature overrides the verifier", func(t *testing.T) {
		verifications, err := GetVerifications(config(t, `
type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  signature: release
  verifier:
    type: RSASigningConfiguration/v1alpha1
- type: signing.config.ocm.software/v1alpha1
  signature: release
  verifier:
    type: SigstoreVerificationConfiguration/v1alpha1
`))
		require.NoError(t, err)
		require.Len(t, verifications, 1, "a signature must be verified once, however many entries name it")
		assert.Equal(t, "SigstoreVerificationConfiguration/v1alpha1", verifications[0].Verifier.GetType().String(),
			"the last entry naming the signature wins, as signingspec.Merge prescribes")
	})

	t.Run("a later entry without a verifier does not clobber an earlier one", func(t *testing.T) {
		verifications, err := GetVerifications(config(t, `
type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  signature: release
  verifier:
    type: SigstoreVerificationConfiguration/v1alpha1
- type: signing.config.ocm.software/v1alpha1
  signature: release
`))
		require.NoError(t, err)
		require.Len(t, verifications, 1)
		assert.Equal(t, "SigstoreVerificationConfiguration/v1alpha1", verifications[0].Verifier.GetType().String(),
			"the skipped duplicate must not drop the verifier configured by another entry")
	})

	t.Run("a named entry falls back to the unscoped verifier", func(t *testing.T) {
		verifications, err := GetVerifications(config(t, `
type: generic.config.ocm.software/v1
configurations:
- type: signing.config.ocm.software/v1alpha1
  verifier:
    type: SigstoreVerificationConfiguration/v1alpha1
- type: signing.config.ocm.software/v1alpha1
  signature: release
`))
		require.NoError(t, err)
		require.Len(t, verifications, 1)
		assert.Equal(t, "SigstoreVerificationConfiguration/v1alpha1", verifications[0].Verifier.GetType().String())
	})
}
