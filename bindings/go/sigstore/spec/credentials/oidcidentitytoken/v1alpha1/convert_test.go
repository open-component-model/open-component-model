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

// fakeTrustedRootTyped simulates the sigstore TrustedRoot credential type without
// importing it (avoids a cycle and keeps this test self-contained). The convertScheme
// must reject any registered or unregistered type that is not OIDCIdentityToken or
// DirectCredentials.
type fakeTrustedRootTyped struct{}

func (f *fakeTrustedRootTyped) GetType() runtime.Type {
	return runtime.NewVersionedType("TrustedRoot", "v1alpha1")
}
func (f *fakeTrustedRootTyped) SetType(_ runtime.Type)       {}
func (f *fakeTrustedRootTyped) DeepCopyTyped() runtime.Typed { return &fakeTrustedRootTyped{} }

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
				Type:      VersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
			want: &OIDCIdentityToken{
				Type:      VersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
		},
		{
			name: "DirectCredentials with camelCase keys",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyToken:     "test-token",
					credentialKeyTokenFile: "/path/token",
				},
			},
			want: &OIDCIdentityToken{
				Type:      VersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
		},
		{
			name: "Raw",
			input: &runtime.Raw{
				Type: VersionedType,
				Data: []byte(`{"type":"OIDCIdentityToken/v1alpha1","token":"test-token","tokenFile":"/path/token"}`),
			},
			want: &OIDCIdentityToken{
				Type:      VersionedType,
				Token:     "test-token",
				TokenFile: "/path/token",
			},
		},
		{
			name:    "unknown type returns error",
			input:   &fakeTyped{},
			wantErr: true,
		},
		{
			name:    "TrustedRoot credential rejected",
			input:   &fakeTrustedRootTyped{},
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
