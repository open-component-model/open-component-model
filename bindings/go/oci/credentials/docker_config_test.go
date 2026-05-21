package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/auth"

	"ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCredentialFunc(t *testing.T) {
	tests := []struct {
		name        string
		identity    *identityv1.OCIRegistryIdentity
		credentials *v1.OCICredentials
		hostport    string
		wantErr     bool
		wantEmpty   bool
		wantCred    *auth.Credential // if set, assert exact credential match
	}{
		{
			name: "matching host and port",
			identity: &identityv1.OCIRegistryIdentity{
				Hostname: "example.com",
				Port:     "443",
			},
			credentials: &v1.OCICredentials{
				Username: "testuser",
				Password: "testpass",
			},
			hostport:  "example.com:443",
			wantErr:   false,
			wantEmpty: false,
		},
		{
			name: "mismatching host",
			identity: &identityv1.OCIRegistryIdentity{
				Hostname: "example.com",
				Port:     "443",
			},
			credentials: &v1.OCICredentials{
				Username: "testuser",
				Password: "testpass",
			},
			hostport:  "wrong.com:443",
			wantErr:   false,
			wantEmpty: true,
		},
		{
			name: "mismatching port",
			identity: &identityv1.OCIRegistryIdentity{
				Hostname: "example.com",
				Port:     "443",
			},
			credentials: &v1.OCICredentials{
				Username: "testuser",
				Password: "testpass",
			},
			hostport:  "example.com:80",
			wantErr:   false,
			wantEmpty: true,
		},
		{
			name: "hostport without port",
			identity: &identityv1.OCIRegistryIdentity{
				Hostname: "example.com",
			},
			credentials: &v1.OCICredentials{
				Username: "testuser",
			},
			hostport:  "example.com",
			wantErr:   false,
			wantEmpty: false,
		},
		{
			name: "all credential types",
			identity: &identityv1.OCIRegistryIdentity{
				Hostname: "example.com",
			},
			credentials: &v1.OCICredentials{
				Username:     "testuser",
				Password:     "testpass",
				AccessToken:  "testtoken",
				RefreshToken: "refreshtoken",
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

			assert.Equal(t, tt.credentials.Username, cred.Username)
			assert.Equal(t, tt.credentials.Password, cred.Password)
			assert.Equal(t, tt.credentials.AccessToken, cred.AccessToken)
			assert.Equal(t, tt.credentials.RefreshToken, cred.RefreshToken)
		})
	}
}

func TestCredentialFromTyped(t *testing.T) {
	tests := []struct {
		name        string
		credentials *v1.OCICredentials
		expected    auth.Credential
	}{
		{
			name:        "zero-value credentials",
			credentials: &v1.OCICredentials{},
			expected:    auth.Credential{},
		},
		{
			name: "all fields populated",
			credentials: &v1.OCICredentials{
				Username:     "user",
				Password:     "pass",
				AccessToken:  "atoken",
				RefreshToken: "rtoken",
			},
			expected: auth.Credential{
				Username:     "user",
				Password:     "pass",
				AccessToken:  "atoken",
				RefreshToken: "rtoken",
			},
		},
		{
			name: "only username and password",
			credentials: &v1.OCICredentials{
				Username: "user",
				Password: "pass",
			},
			expected: auth.Credential{
				Username: "user",
				Password: "pass",
			},
		},
		{
			name: "only access token",
			credentials: &v1.OCICredentials{
				AccessToken: "atoken",
			},
			expected: auth.Credential{
				AccessToken: "atoken",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, MapCredentials(tt.credentials))
		})
	}
}

func TestResolveV1DockerConfigCredentials(t *testing.T) {
	tests := []struct {
		name         string
		dockerConfig credentialsv1.DockerConfig
		identity     identityv1.OCIRegistryIdentity
		wantErr      bool
		wantEmpty    bool
		wantNil      bool
		wantCreds    *v1.OCICredentials
	}{
		{
			name:         "missing hostname in identity leads to no credentials",
			dockerConfig: credentialsv1.DockerConfig{},
			identity:     identityv1.OCIRegistryIdentity{},
			wantErr:      false,
		},
		{
			name:         "empty docker config",
			dockerConfig: credentialsv1.DockerConfig{},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "example.com",
			},
			wantErr:   false,
			wantEmpty: true,
		},
		{
			name:         "hostname not in dockerconfig",
			dockerConfig: credentialsv1.DockerConfig{},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "example.com",
			},
			wantNil: true,
		},
		{
			name: "credentials found without port - no fallback needed",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com":{"username":"testuser","password":"testpass"}}}`,
			},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "registry.example.com",
			},
			wantCreds: &v1.OCICredentials{
				Type:     runtime.NewVersionedType(v1.OCICredentialsType, credentialsv1.Version),
				Username: "testuser",
				Password: "testpass",
			},
		},
		{
			name: "docker.io special case",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"https://index.docker.io/v1/":{"username":"testuser","password":"testpass"}}}`,
			},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "docker.io",
			},
			wantCreds: &v1.OCICredentials{
				Type:     runtime.NewVersionedType(v1.OCICredentialsType, credentialsv1.Version),
				Username: "testuser",
				Password: "testpass",
			},
		},
		{
			name: "credentials stored with port - fallback succeeds",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com:5000":{"username":"portuser","password":"portpass"}}}`,
			},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "registry.example.com",
				Port:     "5000",
			},
			wantCreds: &v1.OCICredentials{
				Type:     runtime.NewVersionedType(v1.OCICredentialsType, credentialsv1.Version),
				Username: "portuser",
				Password: "portpass",
			},
		},
		{
			name: "no credentials for hostname, fallback with port also fails",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"other.example.com":{"username":"otheruser","password":"otherpass"}}}`,
			},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "registry.example.com",
				Port:     "5000",
			},
			wantNil: true,
		},
		{
			name: "no credentials for hostname and no port in identity",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"other.example.com":{"username":"otheruser","password":"otherpass"}}}`,
			},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "registry.example.com",
			},
			wantNil: true,
		},
		{
			name: "credentials with auth field - fallback with port",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com:443":{"auth":"dXNlcjpwYXNz"}}}`,
			},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "registry.example.com",
				Port:     "443",
			},
			wantCreds: &v1.OCICredentials{
				Type:     runtime.NewVersionedType(v1.OCICredentialsType, credentialsv1.Version),
				Username: "user",
				Password: "pass",
			},
		},
		{
			name: "credentials with username and password - found via hostname:port fallback",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com:8080":{"username":"fulluser","password":"fullpass"}}}`,
			},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "registry.example.com",
				Port:     "8080",
			},
			wantCreds: &v1.OCICredentials{
				Type:     runtime.NewVersionedType(v1.OCICredentialsType, credentialsv1.Version),
				Username: "fulluser",
				Password: "fullpass",
			},
		},
		{
			name: "credentials exist for both hostname and hostname:port - prefers hostname (no fallback)",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com":{"username":"noport","password":"noport"},"registry.example.com:5000":{"username":"withport","password":"withport"}}}`,
			},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "registry.example.com",
				Port:     "5000",
			},
			wantCreds: &v1.OCICredentials{
				Type:     runtime.NewVersionedType(v1.OCICredentialsType, credentialsv1.Version),
				Username: "noport",
				Password: "noport",
			},
		},
		{
			name: "wrong port in fallback - returns nil",
			dockerConfig: credentialsv1.DockerConfig{
				DockerConfig: `{"auths":{"registry.example.com:9999":{"username":"wrongport","password":"wrongport"}}}`,
			},
			identity: identityv1.OCIRegistryIdentity{
				Hostname: "registry.example.com",
				Port:     "5000",
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := ResolveV1DockerConfigCredentials(t.Context(), tt.dockerConfig, identityv1.ToIdentity(&tt.identity))

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
