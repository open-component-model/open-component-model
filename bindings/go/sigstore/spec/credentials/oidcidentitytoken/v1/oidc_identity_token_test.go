package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestFromDirectCredentials(t *testing.T) {
	typ := runtime.NewVersionedType(OIDCIdentityTokenType, Version)

	tests := []struct {
		name       string
		properties map[string]string
		expected   *OIDCIdentityToken
	}{
		{
			name: "all fields populated",
			properties: map[string]string{
				CredentialKeyToken:     "my-jwt-token",
				CredentialKeyTokenFile: "/path/to/token",
			},
			expected: &OIDCIdentityToken{
				Type:      typ,
				Token:     "my-jwt-token",
				TokenFile: "/path/to/token",
			},
		},
		{
			name:       "empty map",
			properties: map[string]string{},
			expected:   &OIDCIdentityToken{Type: typ},
		},
		{
			name:       "partial fields",
			properties: map[string]string{CredentialKeyToken: "only-token"},
			expected:   &OIDCIdentityToken{Type: typ, Token: "only-token"},
		},
		{
			name:       "ignores unknown properties",
			properties: map[string]string{CredentialKeyToken: "tok", "unknownField": "ignored"},
			expected:   &OIDCIdentityToken{Type: typ, Token: "tok"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, FromDirectCredentials(tt.properties))
		})
	}
}

func TestMustRegisterCredentialType(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	obj, err := scheme.NewObject(runtime.NewVersionedType(OIDCIdentityTokenType, Version))
	r.NoError(err)
	assert.IsType(t, &OIDCIdentityToken{}, obj)

	obj, err = scheme.NewObject(runtime.NewUnversionedType(OIDCIdentityTokenType))
	r.NoError(err)
	assert.IsType(t, &OIDCIdentityToken{}, obj)
}

func TestOIDCIdentityToken_TypedJSONParsing(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	raw := &runtime.Raw{}
	raw.Data = []byte(`{"type":"OIDCIdentityToken/v1","token":"my-jwt","tokenFile":"/path/tok"}`)
	raw.Type = runtime.NewVersionedType(OIDCIdentityTokenType, Version)

	creds := &OIDCIdentityToken{}
	r.NoError(scheme.Convert(raw, creds))

	assert.Equal(t, "my-jwt", creds.Token)
	assert.Equal(t, "/path/tok", creds.TokenFile)
}

func TestOIDCIdentityToken_SchemeConvert(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterCredentialType(scheme)

	original := &OIDCIdentityToken{
		Type:      runtime.NewVersionedType(OIDCIdentityTokenType, Version),
		Token:     "test-token",
		TokenFile: "/tmp/token",
	}

	raw := &runtime.Raw{}
	r.NoError(scheme.Convert(original, raw))

	restored := &OIDCIdentityToken{}
	r.NoError(scheme.Convert(raw, restored))

	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.Token, restored.Token)
	assert.Equal(t, original.TokenFile, restored.TokenFile)
}
