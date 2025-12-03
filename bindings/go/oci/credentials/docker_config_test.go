package credentials

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/auth"

	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCredentialFunc(t *testing.T) {
	tests := []struct {
		name        string
		identity    runtime.Identity
		credentials map[string]string
		hostport    string
		wantErr     bool
		wantEmpty   bool
	}{
		{
			name: "matching host and port",
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "example.com",
				runtime.IdentityAttributePort:     "443",
			},
			credentials: map[string]string{
				CredentialKeyUsername: "testuser",
				CredentialKeyPassword: "testpass",
			},
			hostport:  "example.com:443",
			wantErr:   false,
			wantEmpty: false,
		},
		{
			name: "mismatching host",
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "example.com",
				runtime.IdentityAttributePort:     "443",
			},
			credentials: map[string]string{
				CredentialKeyUsername: "testuser",
				CredentialKeyPassword: "testpass",
			},
			hostport:  "wrong.com:443",
			wantErr:   false,
			wantEmpty: true,
		},
		{
			name: "mismatching port",
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "example.com",
				runtime.IdentityAttributePort:     "443",
			},
			credentials: map[string]string{
				CredentialKeyUsername: "testuser",
				CredentialKeyPassword: "testpass",
			},
			hostport:  "example.com:80",
			wantErr:   false,
			wantEmpty: true,
		},
		{
			name: "invalid hostport",
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "example.com",
			},
			credentials: map[string]string{
				CredentialKeyUsername: "testuser",
			},
			hostport:  "invalid",
			wantErr:   true,
			wantEmpty: false,
		},
		{
			name: "all credential types",
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "example.com",
			},
			credentials: map[string]string{
				CredentialKeyUsername:     "testuser",
				CredentialKeyPassword:     "testpass",
				CredentialKeyAccessToken:  "testtoken",
				CredentialKeyRefreshToken: "refreshtoken",
			},
			hostport:  "example.com:443",
			wantErr:   false,
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credFunc := CredentialFunc(tt.identity, tt.credentials)
			cred, err := credFunc(context.Background(), tt.hostport)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.wantEmpty {
				assert.Equal(t, auth.EmptyCredential, cred)
				return
			}

			if username, ok := tt.credentials[CredentialKeyUsername]; ok {
				assert.Equal(t, username, cred.Username)
			}
			if password, ok := tt.credentials[CredentialKeyPassword]; ok {
				assert.Equal(t, password, cred.Password)
			}
			if token, ok := tt.credentials[CredentialKeyAccessToken]; ok {
				assert.Equal(t, token, cred.AccessToken)
			}
			if refreshToken, ok := tt.credentials[CredentialKeyRefreshToken]; ok {
				assert.Equal(t, refreshToken, cred.RefreshToken)
			}
		})
	}
}

func TestResolveV1DockerConfigCredentials(t *testing.T) {
	tests := []struct {
		name         string
		dockerConfig credentialsv1.DockerConfig
		identity     runtime.Identity
		wantErr      bool
		wantEmpty    bool
		wantNil      bool
	}{
		{
			name:         "missing hostname in identity",
			dockerConfig: credentialsv1.DockerConfig{},
			identity:     runtime.Identity{},
			wantErr:      true,
		},
		{
			name:         "empty docker config",
			dockerConfig: credentialsv1.DockerConfig{},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "example.com",
			},
			wantErr:   false,
			wantEmpty: true,
		},
		{
			name:         "hostname not in dockerconfig",
			dockerConfig: credentialsv1.DockerConfig{},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "example.com",
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := ResolveV1DockerConfigCredentials(t.Context(), tt.dockerConfig, tt.identity)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.wantEmpty {
				assert.Empty(t, creds)
				return
			}

			if tt.wantNil {
				assert.Nil(t, creds)
				return
			}

			// Additional assertions for successful cases can be added here
		})
	}
}

func TestGetStore(t *testing.T) {
	tests := []struct {
		name         string
		dockerConfig credentialsv1.DockerConfig
		wantErr      assert.ErrorAssertionFunc
	}{
		{
			name:         "default docker config",
			dockerConfig: credentialsv1.DockerConfig{},
			wantErr:      assert.NoError,
		},
		{
			name: "invalid docker config file path will only print warning but succeed",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfigFile: "/nonexistent/path/config.json",
			},
			wantErr: assert.NoError,
		},
		{
			name: "invalid docker config content will fail parsing",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: "invalid json content",
			},
			wantErr: assert.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := getStore(t.Context(), tt.dockerConfig)
			tt.wantErr(t, err)
			if err != nil {
				return
			}
			assert.NotNil(t, store)
		})
	}
}
