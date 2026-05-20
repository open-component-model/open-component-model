package v1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type fakeTyped struct{}

func (f *fakeTyped) GetType() runtime.Type        { return runtime.NewUnversionedType("Unknown") }
func (f *fakeTyped) SetType(_ runtime.Type)       {}
func (f *fakeTyped) DeepCopyTyped() runtime.Typed { return &fakeTyped{} }

func TestFromTyped(t *testing.T) {
	typ := runtime.NewVersionedType(RSACredentialsType, Version)

	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal([]byte(
		`{"type":"RSACredentials/v1","privateKeyPEM":"my-key","publicKeyPEMFile":"/path/pub.pem"}`),
		raw))

	tests := []struct {
		name    string
		input   runtime.Typed
		want    *RSACredentials
		wantErr bool
	}{
		{
			name:  "nil",
			input: nil,
			want:  nil,
		},
		{
			name: "RSACredentials passthrough",
			input: &RSACredentials{
				Type:             typ,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
			want: &RSACredentials{
				Type:             typ,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
		},
		{
			name: "DirectCredentials",
			input: &credv1.DirectCredentials{
				Properties: map[string]string{
					CredentialKeyPrivateKeyPEM:    "my-key",
					CredentialKeyPublicKeyPEMFile: "/path/pub.pem",
				},
			},
			want: &RSACredentials{
				Type:             typ,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
		},
		{
			name:  "Raw",
			input: raw,
			want: &RSACredentials{
				Type:             typ,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
		},
		{
			name: "Unstructured",
			input: &runtime.Unstructured{
				Data: map[string]any{
					"type":             typ,
					"privateKeyPEM":    "my-key",
					"publicKeyPEMFile": "/path/pub.pem",
				},
			},
			want: &RSACredentials{
				Type:             typ,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
		},
		{
			name: "Raw with deprecated snake_case keys",
			input: func() *runtime.Raw {
				r := &runtime.Raw{}
				require.NoError(t, json.Unmarshal([]byte(
					`{"type":"RSACredentials/v1","private_key_pem":"my-key","public_key_pem_file":"/path/pub.pem"}`),
					r))
				return r
			}(),
			want: &RSACredentials{
				Type:             typ,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
		},
		{
			name: "Unstructured with deprecated snake_case keys",
			input: &runtime.Unstructured{
				Data: map[string]any{
					"type":                typ,
					"private_key_pem":     "my-key",
					"public_key_pem_file": "/path/pub.pem",
				},
			},
			want: &RSACredentials{
				Type:             typ,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
		},
		{
			name:    "unknown type returns error",
			input:   &fakeTyped{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromTyped(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFromDirectCredentials(t *testing.T) {
	typ := runtime.NewVersionedType(RSACredentialsType, Version)

	tests := []struct {
		name       string
		properties map[string]string
		expected   *RSACredentials
	}{
		{
			name: "all camelCase fields",
			properties: map[string]string{
				CredentialKeyPublicKeyPEM:      "test-public-key",
				CredentialKeyPublicKeyPEMFile:  "/path/to/public.pem",
				CredentialKeyPrivateKeyPEM:     "test-private-key",
				CredentialKeyPrivateKeyPEMFile: "/path/to/private.pem",
			},
			expected: &RSACredentials{
				Type:              typ,
				PublicKeyPEM:      "test-public-key",
				PublicKeyPEMFile:  "/path/to/public.pem",
				PrivateKeyPEM:     "test-private-key",
				PrivateKeyPEMFile: "/path/to/private.pem",
			},
		},
		{
			name: "deprecated snake_case fields",
			properties: map[string]string{
				DeprecatedCredentialKeyPublicKeyPEM:      "test-public-key",
				DeprecatedCredentialKeyPublicKeyPEMFile:  "/path/to/public.pem",
				DeprecatedCredentialKeyPrivateKeyPEM:     "test-private-key",
				DeprecatedCredentialKeyPrivateKeyPEMFile: "/path/to/private.pem",
			},
			expected: &RSACredentials{
				Type:              typ,
				PublicKeyPEM:      "test-public-key",
				PublicKeyPEMFile:  "/path/to/public.pem",
				PrivateKeyPEM:     "test-private-key",
				PrivateKeyPEMFile: "/path/to/private.pem",
			},
		},
		{
			name: "camelCase takes precedence over deprecated snake_case",
			properties: map[string]string{
				CredentialKeyPrivateKeyPEM:           "camel-key",
				DeprecatedCredentialKeyPrivateKeyPEM: "snake-key",
			},
			expected: &RSACredentials{Type: typ, PrivateKeyPEM: "camel-key"},
		},
		{
			name:       "empty map",
			properties: map[string]string{},
			expected:   &RSACredentials{Type: typ},
		},
		{
			name: "partial fields",
			properties: map[string]string{
				CredentialKeyPrivateKeyPEM: "only-private-key",
			},
			expected: &RSACredentials{Type: typ, PrivateKeyPEM: "only-private-key"},
		},
		{
			name: "ignores unknown properties",
			properties: map[string]string{
				CredentialKeyPrivateKeyPEM: "my-key",
				"unknownField":             "ignored",
			},
			expected: &RSACredentials{Type: typ, PrivateKeyPEM: "my-key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, FromDirectCredentials(tt.properties))
		})
	}
}

func TestMustRegisterCredentialType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	obj, err := scheme.NewObject(runtime.NewVersionedType(RSACredentialsType, Version))
	require.NoError(t, err)
	assert.IsType(t, &RSACredentials{}, obj)

	obj, err = scheme.NewObject(runtime.NewUnversionedType(RSACredentialsType))
	require.NoError(t, err)
	assert.IsType(t, &RSACredentials{}, obj)
}

func TestRSACredentials_TypedJSONParsing(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	// Simulates a user-written inline typed credential in .ocmconfig:
	//   type: RSACredentials/v1
	//   privateKeyPEM: "my-key"
	raw := &runtime.Raw{}
	raw.Data = []byte(`{"type":"RSACredentials/v1","privateKeyPEM":"my-key","publicKeyPEMFile":"/path/pub.pem"}`)
	raw.Type = runtime.NewVersionedType(RSACredentialsType, Version)

	creds := &RSACredentials{}
	require.NoError(t, scheme.Convert(raw, creds))

	assert.Equal(t, "my-key", creds.PrivateKeyPEM)
	assert.Equal(t, "/path/pub.pem", creds.PublicKeyPEMFile)
	assert.Empty(t, creds.PublicKeyPEM)
	assert.Empty(t, creds.PrivateKeyPEMFile)
}

func TestRSACredentials_SchemeConvert(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterCredentialType(scheme)

	original := &RSACredentials{
		Type:              runtime.NewVersionedType(RSACredentialsType, Version),
		PublicKeyPEM:      "test-public-key",
		PublicKeyPEMFile:  "/path/to/public.pem",
		PrivateKeyPEM:     "test-private-key",
		PrivateKeyPEMFile: "/path/to/private.pem",
	}

	raw := &runtime.Raw{}
	require.NoError(t, scheme.Convert(original, raw))

	restored := &RSACredentials{}
	require.NoError(t, scheme.Convert(raw, restored))

	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.PublicKeyPEM, restored.PublicKeyPEM)
	assert.Equal(t, original.PublicKeyPEMFile, restored.PublicKeyPEMFile)
	assert.Equal(t, original.PrivateKeyPEM, restored.PrivateKeyPEM)
	assert.Equal(t, original.PrivateKeyPEMFile, restored.PrivateKeyPEMFile)
}
