package v1alpha1

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

func TestConvertToOIDCIdentityToken(t *testing.T) {
	tests := []struct {
		name    string
		input   runtime.Typed
		want    *OIDCIdentityToken
		wantErr bool
	}{
		{
			name: "OIDCIdentityToken passthrough",
			input: &OIDCIdentityToken{
				Type:      OIDCIdentityTokenVersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
			want: &OIDCIdentityToken{
				Type:      OIDCIdentityTokenVersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
		},
		{
			name: "DirectCredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyToken:     "test-token",
					credentialKeyTokenFile: "/path/token",
				},
			},
			want: &OIDCIdentityToken{
				Type:      OIDCIdentityTokenVersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
		},
		{
			name: "DirectCredentials with deprecated snake_case tokenFile key",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyToken:               "test-token",
					deprecatedCredentialKeyTokenFile: "/path/token",
				},
			},
			want: &OIDCIdentityToken{
				Type:      OIDCIdentityTokenVersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
		},
		{
			name: "Raw",
			input: &runtime.Raw{
				Type: OIDCIdentityTokenVersionedType,
				Data: []byte(`{"type":"OIDCIdentityToken/v1alpha1","token":"test-token","tokenFile":"/path/token"}`),
			},
			want: &OIDCIdentityToken{
				Type:      OIDCIdentityTokenVersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
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
			got, err := ConvertToOIDCIdentityToken(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
