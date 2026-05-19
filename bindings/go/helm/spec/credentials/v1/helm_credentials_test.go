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
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal([]byte(
		`{"type":"HelmHTTPCredentials/v1","username":"user","password":"pass","certFile":"/cert","keyFile":"/key","keyring":"/ring"}`),
		raw))

	tests := []struct {
		name    string
		input   runtime.Typed
		want    *HelmHTTPCredentials
		wantErr bool
	}{
		{
			name:  "nil",
			input: nil,
			want:  nil,
		},
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
				Properties: map[string]string{
					CredentialKeyUsername: "user",
					CredentialKeyPassword: "pass",
					CredentialKeyCertFile: "/cert",
					CredentialKeyKeyFile:  "/key",
					CredentialKeyKeyring:  "/ring",
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
			name:  "Raw",
			input: raw,
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
			name: "Unstructured",
			input: &runtime.Unstructured{
				Data: map[string]any{
					"type":     runtime.NewVersionedType(HelmHTTPCredentialsType, Version),
					"username": "user",
					"password": "pass",
					"certFile": "/cert",
					"keyFile":  "/key",
					"keyring":  "/ring",
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
	props := map[string]string{
		"username": "myuser",
		"password": "mypass",
		"certFile": "/path/to/cert.pem",
		"keyFile":  "/path/to/key.pem",
		"keyring":  "/path/to/keyring",
	}

	creds := FromDirectCredentials(props)

	assert.Equal(t, "myuser", creds.Username)
	assert.Equal(t, "mypass", creds.Password)
	assert.Equal(t, "/path/to/cert.pem", creds.CertFile)
	assert.Equal(t, "/path/to/key.pem", creds.KeyFile)
	assert.Equal(t, "/path/to/keyring", creds.Keyring)
	assert.Equal(t, runtime.NewVersionedType(HelmHTTPCredentialsType, Version), creds.Type)
}

func TestFromDirectCredentials_PartialFields(t *testing.T) {
	props := map[string]string{
		"username": "user",
	}

	creds := FromDirectCredentials(props)

	assert.Equal(t, "user", creds.Username)
	assert.Empty(t, creds.Password)
	assert.Empty(t, creds.CertFile)
	assert.Empty(t, creds.KeyFile)
	assert.Empty(t, creds.Keyring)
}

func TestFromDirectCredentials_EmptyMap(t *testing.T) {
	creds := FromDirectCredentials(map[string]string{})

	assert.Equal(t, runtime.NewVersionedType(HelmHTTPCredentialsType, Version), creds.Type)
	assert.Empty(t, creds.Username)
	assert.Empty(t, creds.Password)
	assert.Empty(t, creds.CertFile)
	assert.Empty(t, creds.KeyFile)
	assert.Empty(t, creds.Keyring)
}

func TestFromDirectCredentials_NilMap(t *testing.T) {
	creds := FromDirectCredentials(nil)

	assert.Equal(t, runtime.NewVersionedType(HelmHTTPCredentialsType, Version), creds.Type)
	assert.Empty(t, creds.Username)
	assert.Empty(t, creds.Password)
	assert.Empty(t, creds.CertFile)
	assert.Empty(t, creds.KeyFile)
	assert.Empty(t, creds.Keyring)
}

