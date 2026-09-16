package v1

import (
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

func TestConvertToWgetCredentials(t *testing.T) {
	tests := []struct {
		name        string
		input       runtime.Typed
		want        *WgetCredentials
		wantErr     bool
		errContains string
	}{
		{
			name: "WgetCredentials passthrough",
			input: &WgetCredentials{
				Type:                 WgetCredentialsVersionedType,
				Username:             "user",
				Password:             "pass",
				IdentityToken:        "token",
				Certificate:          "/cert",
				PrivateKey:           "/key",
				CertificateAuthority: "/ca",
			},
			want: &WgetCredentials{
				Type:                 WgetCredentialsVersionedType,
				Username:             "user",
				Password:             "pass",
				IdentityToken:        "token",
				Certificate:          "/cert",
				PrivateKey:           "/key",
				CertificateAuthority: "/ca",
			},
		},
		{
			name: "DirectCredentials maps to WgetCredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyUsername:             "user",
					credentialKeyPassword:             "pass",
					credentialKeyIdentityToken:        "token",
					credentialKeyCertificate:          "/cert",
					credentialKeyPrivateKey:           "/key",
					credentialKeyCertificateAuthority: "/ca",
				},
			},
			want: &WgetCredentials{
				Type:                 WgetCredentialsVersionedType,
				Username:             "user",
				Password:             "pass",
				IdentityToken:        "token",
				Certificate:          "/cert",
				PrivateKey:           "/key",
				CertificateAuthority: "/ca",
			},
		},
		{
			name: "DirectCredentials username/password only maps to WgetCredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyUsername: "user",
					credentialKeyPassword: "pass",
				},
			},
			want: &WgetCredentials{
				Type:     WgetCredentialsVersionedType,
				Username: "user",
				Password: "pass",
			},
		},
		{
			name: "DirectCredentials mTLS fields only maps to WgetCredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyCertificate:          "/cert",
					credentialKeyPrivateKey:           "/key",
					credentialKeyCertificateAuthority: "/ca",
				},
			},
			want: &WgetCredentials{
				Type:                 WgetCredentialsVersionedType,
				Certificate:          "/cert",
				PrivateKey:           "/key",
				CertificateAuthority: "/ca",
			},
		},
		{
			name: "Raw with explicit WgetCredentials type",
			input: &runtime.Raw{
				Type: WgetCredentialsVersionedType,
				Data: []byte(`{"type":"WgetCredentials/v1","username":"user","password":"pass","identityToken":"token","certificate":"/cert","privateKey":"/key","certificateAuthority":"/ca"}`),
			},
			want: &WgetCredentials{
				Type:                 WgetCredentialsVersionedType,
				Username:             "user",
				Password:             "pass",
				IdentityToken:        "token",
				Certificate:          "/cert",
				PrivateKey:           "/key",
				CertificateAuthority: "/ca",
			},
		},
		{
			name: "Raw WgetCredentials with legacy properties field is rejected",
			input: &runtime.Raw{
				Type: WgetCredentialsVersionedType,
				Data: []byte(`{"type":"WgetCredentials/v1","properties":{"username":"user","password":"pass"}}`),
			},
			wantErr:     true,
			errContains: `unknown field "properties"`,
		},
		{
			name: "Raw legacy Credentials/v1 with free-form properties converts via DirectCredentials",
			input: &runtime.Raw{
				Type: runtime.NewVersionedType(credv1.CredentialsType, credv1.Version),
				Data: []byte(`{"type":"Credentials/v1","properties":{"username":"user","password":"pass","other":"kept"}}`),
			},
			want: &WgetCredentials{
				Type:     WgetCredentialsVersionedType,
				Username: "user",
				Password: "pass",
			},
		},
		{
			name:    "unknown type returns error",
			input:   &fakeTyped{},
			wantErr: true,
		},
		{
			name: "empty WgetCredentials is rejected at the conversion boundary",
			input: &runtime.Raw{
				Type: WgetCredentialsVersionedType,
				Data: []byte(`{"type":"WgetCredentials/v1"}`),
			},
			wantErr:     true,
			errContains: "no authentication material",
		},
		{
			name: "password without username is rejected at the conversion boundary",
			input: &runtime.Raw{
				Type: WgetCredentialsVersionedType,
				Data: []byte(`{"type":"WgetCredentials/v1","password":"pass"}`),
			},
			wantErr:     true,
			errContains: "password is set but username is empty",
		},
		{
			name: "certificate without privateKey is rejected at the conversion boundary",
			input: &runtime.Raw{
				Type: WgetCredentialsVersionedType,
				Data: []byte(`{"type":"WgetCredentials/v1","certificate":"/cert"}`),
			},
			wantErr:     true,
			errContains: "certificate is set but privateKey is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertToWgetCredentials(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
