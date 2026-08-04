package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing/v1alpha1/spec"
)

func rsaSignerType() runtime.Type {
	return runtime.NewVersionedType("RSASigningConfiguration", "v1alpha1")
}

func sigstoreSignerType() runtime.Type {
	return runtime.NewVersionedType("SigstoreSigningConfiguration", "v1alpha1")
}

func TestConfig_ParseYAML(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantSigner   *runtime.Type
		wantVerifier *runtime.Type
	}{
		{
			name: "signer and verifier",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: signing.config.ocm.software/v1alpha1
    signer:
      type: RSASigningConfiguration/v1alpha1
      signatureAlgorithm: RSASSA-PSS
      signatureEncodingPolicy: Plain
    verifier:
      type: RSASigningConfiguration/v1alpha1
`,
			wantSigner:   ptr(rsaSignerType()),
			wantVerifier: ptr(rsaSignerType()),
		},
		{
			name: "signer only",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: signing.config.ocm.software/v1alpha1
    signer:
      type: SigstoreSigningConfiguration/v1alpha1
`,
			wantSigner: ptr(sigstoreSignerType()),
		},
		{
			name: "fields omitted stay nil",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: signing.config.ocm.software/v1alpha1
`,
		},
		{
			name: "unversioned type alias",
			yaml: `
type: generic.config.ocm.software/v1
configurations:
  - type: signing.config.ocm.software
    signer:
      type: RSASigningConfiguration/v1alpha1
`,
			wantSigner: ptr(rsaSignerType()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var generic genericv1.Config
			err := genericv1.Scheme.Decode(strings.NewReader(tt.yaml), &generic)
			require.NoError(t, err)
			require.Len(t, generic.Configurations, 1)

			var cfg spec.Config
			err = spec.Scheme.Convert(generic.Configurations[0], &cfg)
			require.NoError(t, err)

			if tt.wantSigner == nil {
				assert.Nil(t, cfg.Signer)
			} else {
				require.NotNil(t, cfg.Signer)
				assert.Equal(t, *tt.wantSigner, cfg.Signer.GetType())
			}
			if tt.wantVerifier == nil {
				assert.Nil(t, cfg.Verifier)
			} else {
				require.NotNil(t, cfg.Verifier)
				assert.Equal(t, *tt.wantVerifier, cfg.Verifier.GetType())
			}
		})
	}
}

func TestConfig_ParseYAML_PreservesSignerFields(t *testing.T) {
	yaml := `
type: generic.config.ocm.software/v1
configurations:
  - type: signing.config.ocm.software/v1alpha1
    signer:
      type: RSASigningConfiguration/v1alpha1
      signatureAlgorithm: RSASSA-PSS
      signatureEncodingPolicy: PEM
`
	var generic genericv1.Config
	require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(yaml), &generic))

	var cfg spec.Config
	require.NoError(t, spec.Scheme.Convert(generic.Configurations[0], &cfg))
	require.NotNil(t, cfg.Signer)

	// The signer spec is carried through opaquely: its handler-specific fields
	// (algorithm, encoding policy) survive the round-trip so the consumer can
	// resolve them at the point of use.
	assert.Contains(t, string(cfg.Signer.Data), "RSASSA-PSS")
	assert.Contains(t, string(cfg.Signer.Data), "PEM")
}

func TestConfig_Validate(t *testing.T) {
	rawWithType := &runtime.Raw{Type: rsaSignerType(), Data: []byte(`{"type":"RSASigningConfiguration/v1alpha1"}`)}
	rawWithoutType := &runtime.Raw{Data: []byte(`{}`)}

	tests := []struct {
		name    string
		cfg     spec.Config
		wantErr string
	}{
		{"valid empty", spec.Config{}, ""},
		{"valid signer", spec.Config{Signer: rawWithType}, ""},
		{"valid signer and verifier", spec.Config{Signer: rawWithType, Verifier: rawWithType}, ""},
		{"valid explicit type", spec.Config{Type: runtime.NewVersionedType(spec.ConfigType, spec.Version)}, ""},
		{"wrong type name", spec.Config{Type: runtime.NewUnversionedType("other.config.ocm.software")}, "invalid type"},
		{"wrong type version", spec.Config{Type: runtime.NewVersionedType(spec.ConfigType, "v2")}, "invalid type"},
		{"signer without type", spec.Config{Signer: rawWithoutType}, "signer specification must have a type"},
		{"verifier without type", spec.Config{Verifier: rawWithoutType}, "verifier specification must have a type"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	rsa := &runtime.Raw{Type: rsaSignerType(), Data: []byte(`{"type":"RSASigningConfiguration/v1alpha1"}`)}
	sigstore := &runtime.Raw{Type: sigstoreSignerType(), Data: []byte(`{"type":"SigstoreSigningConfiguration/v1alpha1"}`)}

	t.Run("empty returns nil", func(t *testing.T) {
		assert.Nil(t, spec.Merge())
	})

	t.Run("later non-nil fields win", func(t *testing.T) {
		a := &spec.Config{Signer: rsa, Verifier: rsa}
		b := &spec.Config{Signer: sigstore}

		merged := spec.Merge(a, b)
		require.NotNil(t, merged)
		assert.Equal(t, sigstoreSignerType(), merged.Signer.GetType())
		// Verifier not set on b, so a's value falls through.
		assert.Equal(t, rsaSignerType(), merged.Verifier.GetType())
		assert.Equal(t, runtime.NewVersionedType(spec.ConfigType, spec.Version), merged.Type)
	})

	t.Run("nil element is skipped", func(t *testing.T) {
		a := &spec.Config{Signer: rsa}

		merged := spec.Merge(nil, a, nil)
		require.NotNil(t, merged)
		assert.Equal(t, rsaSignerType(), merged.Signer.GetType())
	})
}

func TestLookupConfig(t *testing.T) {
	decode := func(t *testing.T, yaml string) *genericv1.Config {
		t.Helper()
		var generic genericv1.Config
		require.NoError(t, genericv1.Scheme.Decode(strings.NewReader(yaml), &generic))
		return &generic
	}

	t.Run("nil config", func(t *testing.T) {
		cfg, err := spec.LookupConfig(nil)
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("no signing entries", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: other.config.ocm.software/v1
`)
		cfg, err := spec.LookupConfig(generic)
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("single entry", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: signing.config.ocm.software/v1alpha1
    signer:
      type: RSASigningConfiguration/v1alpha1
`)
		cfg, err := spec.LookupConfig(generic)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		require.NotNil(t, cfg.Signer)
		assert.Equal(t, rsaSignerType(), cfg.Signer.GetType())
		assert.Nil(t, cfg.Verifier)
	})

	t.Run("later entry wins, unset fields fall through", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: signing.config.ocm.software/v1alpha1
    signer:
      type: RSASigningConfiguration/v1alpha1
    verifier:
      type: RSASigningConfiguration/v1alpha1
  - type: signing.config.ocm.software/v1alpha1
    signer:
      type: SigstoreSigningConfiguration/v1alpha1
`)
		cfg, err := spec.LookupConfig(generic)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, sigstoreSignerType(), cfg.Signer.GetType())
		assert.Equal(t, rsaSignerType(), cfg.Verifier.GetType())
	})

	t.Run("invalid entry is rejected", func(t *testing.T) {
		generic := decode(t, `
type: generic.config.ocm.software/v1
configurations:
  - type: signing.config.ocm.software/v1alpha1
    signer:
      notype: true
`)
		_, err := spec.LookupConfig(generic)
		require.ErrorContains(t, err, "signer specification must have a type")
	})
}

func ptr[T any](v T) *T { return &v }
