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

func TestConvertToOCICredentials(t *testing.T) {
	tests := []struct {
		name    string
		input   runtime.Typed
		want    *OCICredentials
		wantErr bool
	}{
		{
			name: "OCICredentials passthrough",
			input: &OCICredentials{
				Type:         runtime.NewVersionedType(OCICredentialsType, Version),
				Username:     "user",
				Password:     "pass",
				AccessToken:  "tok",
				RefreshToken: "ref",
			},
			want: &OCICredentials{
				Type:         runtime.NewVersionedType(OCICredentialsType, Version),
				Username:     "user",
				Password:     "pass",
				AccessToken:  "tok",
				RefreshToken: "ref",
			},
		},
		{
			name: "DirectCredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyUsername:     "user",
					credentialKeyPassword:     "pass",
					credentialKeyAccessToken:  "tok",
					credentialKeyRefreshToken: "ref",
				},
			},
			want: &OCICredentials{
				Type:         runtime.NewVersionedType(OCICredentialsType, Version),
				Username:     "user",
				Password:     "pass",
				AccessToken:  "tok",
				RefreshToken: "ref",
			},
		},
		{
			name: "Raw",
			input: &runtime.Raw{
				Type: runtime.NewVersionedType(OCICredentialsType, Version),
				Data: []byte(`{"type":"OCICredentials/v1","username":"user","password":"pass","accessToken":"tok","refreshToken":"ref"}`),
			},
			want: &OCICredentials{
				Type:         runtime.NewVersionedType(OCICredentialsType, Version),
				Username:     "user",
				Password:     "pass",
				AccessToken:  "tok",
				RefreshToken: "ref",
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
			got, err := ConvertToOCICredentials(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
