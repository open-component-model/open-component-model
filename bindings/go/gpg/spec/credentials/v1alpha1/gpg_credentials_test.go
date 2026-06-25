package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFromDirectCredentials(t *testing.T) {
	typ := runtime.NewVersionedType(GPGCredentialsType, Version)

	tests := []struct {
		name       string
		properties map[string]string
		expected   *GPGCredentials
	}{
		{
			name: "all fields populated",
			properties: map[string]string{
				"privateKeyPGP":     "test-private-key",
				"privateKeyPGPFile": "/path/to/private.asc",
				"publicKeyPGP":      "test-public-key",
				"publicKeyPGPFile":  "/path/to/public.asc",
				"passphrase":        "test-passphrase-value",
			},
			expected: &GPGCredentials{
				Type:              typ,
				PrivateKeyPGP:     "test-private-key",
				PrivateKeyPGPFile: "/path/to/private.asc",
				PublicKeyPGP:      "test-public-key",
				PublicKeyPGPFile:  "/path/to/public.asc",
				Passphrase:        "test-passphrase-value",
			},
		},
		{
			name:       "empty map",
			properties: map[string]string{},
			expected:   &GPGCredentials{Type: typ},
		},
		{
			name: "partial fields",
			properties: map[string]string{
				"privateKeyPGP": "only-private-key",
			},
			expected: &GPGCredentials{Type: typ, PrivateKeyPGP: "only-private-key"},
		},
		{
			name: "ignores unknown properties",
			properties: map[string]string{
				"privateKeyPGP": "my-key",
				"unknownField":  "ignored",
			},
			expected: &GPGCredentials{Type: typ, PrivateKeyPGP: "my-key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, fromDirectCredentials(tt.properties))
		})
	}
}

func TestMustRegisterCredentialType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	obj, err := scheme.NewObject(runtime.NewVersionedType(GPGCredentialsType, Version))
	require.NoError(t, err)
	assert.IsType(t, &GPGCredentials{}, obj)

	obj, err = scheme.NewObject(runtime.NewUnversionedType(GPGCredentialsType))
	require.NoError(t, err)
	assert.IsType(t, &GPGCredentials{}, obj)
}

func TestGPGCredentials_TypedJSONParsing(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	raw := &runtime.Raw{}
	raw.Data = []byte(`{"type":"GPGCredentials/v1alpha1","privateKeyPGP":"my-key","publicKeyPGPFile":"/path/pub.asc"}`)
	raw.Type = runtime.NewVersionedType(GPGCredentialsType, Version)

	creds := &GPGCredentials{}
	require.NoError(t, scheme.Convert(raw, creds))

	assert.Equal(t, "my-key", creds.PrivateKeyPGP)
	assert.Equal(t, "/path/pub.asc", creds.PublicKeyPGPFile)
	assert.Empty(t, creds.PublicKeyPGP)
	assert.Empty(t, creds.PrivateKeyPGPFile)
}

func TestGPGCredentials_SchemeConvert(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterCredentialType(scheme)

	original := &GPGCredentials{
		Type:              runtime.NewVersionedType(GPGCredentialsType, Version),
		PrivateKeyPGP:     "test-private-key",
		PrivateKeyPGPFile: "/path/to/private.asc",
		PublicKeyPGP:      "test-public-key",
		PublicKeyPGPFile:  "/path/to/public.asc",
	}

	raw := &runtime.Raw{}
	require.NoError(t, scheme.Convert(original, raw))

	restored := &GPGCredentials{}
	require.NoError(t, scheme.Convert(raw, restored))

	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.PrivateKeyPGP, restored.PrivateKeyPGP)
	assert.Equal(t, original.PrivateKeyPGPFile, restored.PrivateKeyPGPFile)
	assert.Equal(t, original.PublicKeyPGP, restored.PublicKeyPGP)
	assert.Equal(t, original.PublicKeyPGPFile, restored.PublicKeyPGPFile)
}
