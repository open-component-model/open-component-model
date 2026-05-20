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
					credentialKeyPrivateKeyPEM:    "my-key",
					credentialKeyPublicKeyPEMFile: "/path/pub.pem",
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
				credentialKeyPublicKeyPEM:      "test-public-key",
				credentialKeyPublicKeyPEMFile:  "/path/to/public.pem",
				credentialKeyPrivateKeyPEM:     "test-private-key",
				credentialKeyPrivateKeyPEMFile: "/path/to/private.pem",
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
				deprecatedCredentialKeyPublicKeyPEM:      "test-public-key",
				deprecatedCredentialKeyPublicKeyPEMFile:  "/path/to/public.pem",
				deprecatedCredentialKeyPrivateKeyPEM:     "test-private-key",
				deprecatedCredentialKeyPrivateKeyPEMFile: "/path/to/private.pem",
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
				credentialKeyPrivateKeyPEM:           "camel-key",
				deprecatedCredentialKeyPrivateKeyPEM: "snake-key",
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
				credentialKeyPrivateKeyPEM: "only-private-key",
			},
			expected: &RSACredentials{Type: typ, PrivateKeyPEM: "only-private-key"},
		},
		{
			name: "ignores unknown properties",
			properties: map[string]string{
				credentialKeyPrivateKeyPEM: "my-key",
				"unknownField":             "ignored",
			},
			expected: &RSACredentials{Type: typ, PrivateKeyPEM: "my-key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, fromDirectCredentials(tt.properties))
		})
	}
}
