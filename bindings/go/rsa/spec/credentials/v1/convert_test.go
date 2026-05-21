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

func TestConvertToRSACredentials(t *testing.T) {
	tests := []struct {
		name    string
		input   runtime.Typed
		want    *RSACredentials
		wantErr bool
	}{
		{
			name: "RSACredentials passthrough",
			input: &RSACredentials{
				Type:             VersionedType,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
			want: &RSACredentials{
				Type:             VersionedType,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
		},
		{
			name: "DirectCredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, Version),
				Properties: map[string]string{
					credentialKeyPrivateKeyPEM:    "my-key",
					credentialKeyPublicKeyPEMFile: "/path/pub.pem",
				},
			},
			want: &RSACredentials{
				Type:             VersionedType,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
		},
		{
			name: "DirectCredentials with deprecated snake_case keys",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, Version),
				Properties: map[string]string{
					deprecatedCredentialKeyPrivateKeyPEM:    "my-key",
					deprecatedCredentialKeyPublicKeyPEMFile: "/path/pub.pem",
				},
			},
			want: &RSACredentials{
				Type:             VersionedType,
				PrivateKeyPEM:    "my-key",
				PublicKeyPEMFile: "/path/pub.pem",
			},
		},
		{
			name: "Raw",
			input: &runtime.Raw{
				Type: VersionedType,
				Data: []byte(`{"type":"RSACredentials/v1","privateKeyPEM":"my-key","publicKeyPEMFile":"/path/pub.pem"}`),
			},
			want: &RSACredentials{
				Type:             VersionedType,
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
			got, err := ConvertToRSACredentials(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
