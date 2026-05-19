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
		`{"type":"OCICredentials/v1","username":"user","password":"pass","accessToken":"tok","refreshToken":"ref"}`),
		raw))

	tests := []struct {
		name    string
		input   runtime.Typed
		want    *OCICredentials
		wantErr bool
	}{
		{
			name:  "nil",
			input: nil,
			want:  nil,
		},
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
				Properties: map[string]string{
					CredentialKeyUsername:     "user",
					CredentialKeyPassword:     "pass",
					CredentialKeyAccessToken:  "tok",
					CredentialKeyRefreshToken: "ref",
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
			name:  "Raw",
			input: raw,
			want: &OCICredentials{
				Type:         runtime.NewVersionedType(OCICredentialsType, Version),
				Username:     "user",
				Password:     "pass",
				AccessToken:  "tok",
				RefreshToken: "ref",
			},
		},
		{
			name: "Unstructured",
			input: &runtime.Unstructured{
				Data: map[string]any{
					"type":         runtime.NewVersionedType(OCICredentialsType, Version),
					"username":     "user",
					"password":     "pass",
					"accessToken":  "tok",
					"refreshToken": "ref",
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
	tests := []struct {
		name       string
		properties map[string]string
		expected   *OCICredentials
	}{
		{
			name: "all fields populated",
			properties: map[string]string{
				"username":     "myuser",
				"password":     "mypass",
				"accessToken":  "token123",
				"refreshToken": "refresh456",
			},
			expected: &OCICredentials{
				Type:         runtime.NewVersionedType(OCICredentialsType, Version),
				Username:     "myuser",
				Password:     "mypass",
				AccessToken:  "token123",
				RefreshToken: "refresh456",
			},
		},
		{
			name: "only username and password",
			properties: map[string]string{
				"username": "myuser",
				"password": "mypass",
			},
			expected: &OCICredentials{
				Type:     runtime.NewVersionedType(OCICredentialsType, Version),
				Username: "myuser",
				Password: "mypass",
			},
		},
		{
			name:       "empty properties",
			properties: map[string]string{},
			expected: &OCICredentials{
				Type: runtime.NewVersionedType(OCICredentialsType, Version),
			},
		},
		{
			name: "ignores unknown properties",
			properties: map[string]string{
				"username":    "myuser",
				"unknownProp": "value",
			},
			expected: &OCICredentials{
				Type:     runtime.NewVersionedType(OCICredentialsType, Version),
				Username: "myuser",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FromDirectCredentials(tt.properties)
			assert.Equal(t, tt.expected, result)
		})
	}
}

