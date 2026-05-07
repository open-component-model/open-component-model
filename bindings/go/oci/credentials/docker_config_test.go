package credentials

import (
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
		wantCred    *auth.Credential // if set, assert exact credential match
	}{
		{
			name: "matching host and port",
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "example.com",
				runtime.IdentityAttributePort:     "443",
			},
			credentials: map[string]string{
				"username": "testuser",
				"password": "testpass",
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
				"username": "testuser",
				"password": "testpass",
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
				"username": "testuser",
				"password": "testpass",
			},
			hostport:  "example.com:80",
			wantErr:   false,
			wantEmpty: true,
		},
		{
			name: "hostport without port",
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "example.com",
			},
			credentials: map[string]string{
				"username": "testuser",
			},
			hostport:  "example.com",
			wantErr:   false,
			wantEmpty: false,
		},
		{
			name: "all credential types",
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "example.com",
			},
			credentials: map[string]string{
				"username":     "testuser",
				"password":     "testpass",
				"accessToken":  "testtoken",
				"refreshToken": "refreshtoken",
			},
			hostport:  "example.com:443",
			wantErr:   false,
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credFunc := CredentialFunc(tt.identity, tt.credentials)
			cred, err := credFunc(t.Context(), tt.hostport)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.wantEmpty {
				assert.Equal(t, auth.EmptyCredential, cred)
				return
			}

			if tt.wantCred != nil {
				assert.Equal(t, *tt.wantCred, cred)
				return
			}

			if username, ok := tt.credentials["username"]; ok {
				assert.Equal(t, username, cred.Username)
			}
			if password, ok := tt.credentials["password"]; ok {
				assert.Equal(t, password, cred.Password)
			}
			if token, ok := tt.credentials["accessToken"]; ok {
				assert.Equal(t, token, cred.AccessToken)
			}
			if refreshToken, ok := tt.credentials["refreshToken"]; ok {
				assert.Equal(t, refreshToken, cred.RefreshToken)
			}
		})
	}
}

func TestCredentialFromMap(t *testing.T) {
	tests := []struct {
		name        string
		credentials map[string]string
		expected    auth.Credential
	}{
		{
			name:        "empty credentials",
			credentials: map[string]string{},
			expected:    auth.Credential{},
		},
		{
			name: "camelCase keys (canonical)",
			credentials: map[string]string{
				"username":     "user",
				"password":     "pass",
				"accessToken":  "atoken",
				"refreshToken": "rtoken",
			},
			expected: auth.Credential{
				Username:     "user",
				Password:     "pass",
				AccessToken:  "atoken",
				RefreshToken: "rtoken",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CredentialFromMap(tt.credentials)
			assert.Equal(t, tt.expected, result)
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
		wantCreds    map[string]string
	}{
		{
			name:         "missing hostname in identity leads to no credentials",
			dockerConfig: credentialsv1.DockerConfig{},
			identity:     runtime.Identity{},
			wantErr:      false,
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
		{
			name: "credentials found without port - no fallback needed",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com":{"username":"testuser","password":"testpass"}}}`,
			},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "registry.example.com",
			},
			wantCreds: map[string]string{
				"username": "testuser",
				"password": "testpass",
			},
		},
		{
			name: "docker.io special case",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"https://index.docker.io/v1/":{"username":"testuser","password":"testpass"}}}`,
			},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "docker.io",
			},
			wantCreds: map[string]string{
				"username": "testuser",
				"password": "testpass",
			},
		},
		{
			name: "credentials stored with port - fallback succeeds",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com:5000":{"username":"portuser","password":"portpass"}}}`,
			},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePort:     "5000",
			},
			wantCreds: map[string]string{
				"username": "portuser",
				"password": "portpass",
			},
		},
		{
			name: "no credentials for hostname, fallback with port also fails",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"other.example.com":{"username":"otheruser","password":"otherpass"}}}`,
			},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePort:     "5000",
			},
			wantNil: true,
		},
		{
			name: "no credentials for hostname and no port in identity",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"other.example.com":{"username":"otheruser","password":"otherpass"}}}`,
			},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "registry.example.com",
			},
			wantNil: true,
		},
		{
			name: "credentials with auth field - fallback with port",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com:443":{"auth":"dXNlcjpwYXNz"}}}`,
			},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePort:     "443",
			},
			wantCreds: map[string]string{
				"username": "user",
				"password": "pass",
			},
		},
		{
			name: "credentials with username and password - found via hostname:port fallback",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com:8080":{"username":"fulluser","password":"fullpass"}}}`,
			},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePort:     "8080",
			},
			wantCreds: map[string]string{
				"username": "fulluser",
				"password": "fullpass",
			},
		},
		{
			name: "credentials exist for both hostname and hostname:port - prefers hostname (no fallback)",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com":{"username":"noport","password":"noport"},"registry.example.com:5000":{"username":"withport","password":"withport"}}}`,
			},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePort:     "5000",
			},
			wantCreds: map[string]string{
				"username": "noport",
				"password": "noport",
			},
		},
		{
			name: "wrong port in fallback - returns nil",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com:9999":{"username":"wrongport","password":"wrongport"}}}`,
			},
			identity: runtime.Identity{
				runtime.IdentityAttributeHostname: "registry.example.com",
				runtime.IdentityAttributePort:     "5000",
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

			if tt.wantCreds != nil {
				assert.Equal(t, tt.wantCreds, creds)
				return
			}
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
