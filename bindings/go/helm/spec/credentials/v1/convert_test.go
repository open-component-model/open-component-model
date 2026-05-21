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

func TestConvertToHelmHTTPCredentials(t *testing.T) {
	tests := []struct {
		name    string
		input   runtime.Typed
		want    *HelmHTTPCredentials
		wantErr bool
	}{
		{
			name: "HelmHTTPCredentials passthrough",
			input: &HelmHTTPCredentials{
				Type:     runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
				Username: "user",
				Password: "pass",
				CertFile: "/cert",
				KeyFile:  "/key",
				Keyring:  "/ring",
			},
			want: &HelmHTTPCredentials{
				Type:     runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
				Username: "user",
				Password: "pass",
				CertFile: "/cert",
				KeyFile:  "/key",
				Keyring:  "/ring",
			},
		},
		{
			name: "DirectCredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyUsername: "user",
					credentialKeyPassword: "pass",
					credentialKeyCertFile: "/cert",
					credentialKeyKeyFile:  "/key",
					credentialKeyKeyring:  "/ring",
				},
			},
			want: &HelmHTTPCredentials{
				Type:     runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
				Username: "user",
				Password: "pass",
				CertFile: "/cert",
				KeyFile:  "/key",
				Keyring:  "/ring",
			},
		},
		{
			name: "Raw",
			input: &runtime.Raw{
				Type: runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
				Data: []byte(`{"type":"HelmHTTPCredentials/v1","username":"user","password":"pass","certFile":"/cert","keyFile":"/key","keyring":"/ring"}`),
			},
			want: &HelmHTTPCredentials{
				Type:     runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
				Username: "user",
				Password: "pass",
				CertFile: "/cert",
				KeyFile:  "/key",
				Keyring:  "/ring",
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
			got, err := ConvertToHelmHTTPCredentials(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
