package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
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
			name: "DirectCredentials maps to HelmHTTPCredentials",
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
			name: "Raw with explicit HelmHTTPCredentials type",
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
			name:  "nil input returns nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "raw JSON with no type field returns nil",
			input: &runtime.Raw{Data: []byte(`{"access_token":"tok"}`)},
			want:  nil,
		},
		{
			name:    "OCICredentials returns error (wrong transport)",
			input:   &ocicredsv1.OCICredentials{Type: ocicredsv1.OCICredentialsVersionedType},
			wantErr: true,
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

func TestConvertToOCICredentials(t *testing.T) {
	tests := []struct {
		name    string
		input   runtime.Typed
		want    *ocicredsv1.OCICredentials
		wantErr bool
	}{
		{
			name: "OCICredentials passthrough",
			input: &ocicredsv1.OCICredentials{
				Type:        ocicredsv1.OCICredentialsVersionedType,
				Username:    "user",
				Password:    "pass",
				AccessToken: "token",
			},
			want: &ocicredsv1.OCICredentials{
				Type:        ocicredsv1.OCICredentialsVersionedType,
				Username:    "user",
				Password:    "pass",
				AccessToken: "token",
			},
		},
		{
			name: "DirectCredentials maps to OCICredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyUsername:    "user",
					credentialKeyPassword:    "pass",
					credentialKeyAccessToken: "tok",
				},
			},
			want: &ocicredsv1.OCICredentials{
				Type:        ocicredsv1.OCICredentialsVersionedType,
				Username:    "user",
				Password:    "pass",
				AccessToken: "tok",
			},
		},
		{
			name: "DirectCredentials with refreshToken maps to OCICredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyUsername:     "user",
					credentialKeyRefreshToken: "ref",
				},
			},
			want: &ocicredsv1.OCICredentials{
				Type:         ocicredsv1.OCICredentialsVersionedType,
				Username:     "user",
				RefreshToken: "ref",
			},
		},
		{
			name: "DirectCredentials username/password only maps to OCICredentials",
			input: &credv1.DirectCredentials{
				Type: runtime.NewVersionedType(credv1.DirectCredentialsType, credv1.Version),
				Properties: map[string]string{
					credentialKeyUsername: "user",
					credentialKeyPassword: "pass",
				},
			},
			want: &ocicredsv1.OCICredentials{
				Type:     ocicredsv1.OCICredentialsVersionedType,
				Username: "user",
				Password: "pass",
			},
		},
		{
			name:  "nil input returns nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "raw JSON with no type field returns nil",
			input: &runtime.Raw{Data: []byte(`{"access_token":"tok"}`)},
			want:  nil,
		},
		{
			name:    "HelmHTTPCredentials returns error (wrong transport)",
			input:   &HelmHTTPCredentials{Type: runtime.NewVersionedType(HelmHTTPCredentialsType, Version)},
			wantErr: true,
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
