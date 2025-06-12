package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"oras.land/oras-go/v2/registry/remote/auth"

	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCredentialCache_Get(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*credentialCache)
		hostport string
		want     auth.Credential
		wantErr  bool
	}{
		{
			name: "empty cache returns empty credential",
			setup: func(c *credentialCache) {
				// No setup needed
			},
			hostport: "example.com:443",
			want:     auth.EmptyCredential,
			wantErr:  false,
		},
		{
			name: "returns matching credential",
			setup: func(c *credentialCache) {
				spec := &ocirepospecv1.Repository{BaseUrl: "https://example.com:443"}
				creds := map[string]string{
					"username": "testuser",
					"password": "testpass",
				}
				err := c.add(spec, creds)
				require.NoError(t, err)
			},
			hostport: "example.com:443",
			want: auth.Credential{
				Username: "testuser",
				Password: "testpass",
			},
			wantErr: false,
		},
		{
			name: "invalid hostport returns error",
			setup: func(c *credentialCache) {
				// No setup needed
			},
			hostport: "invalid:host:port",
			want:     auth.EmptyCredential,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &credentialCache{}
			tt.setup(cache)

			got, err := cache.get(context.Background(), tt.hostport)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCredentialCache_Add(t *testing.T) {
	tests := []struct {
		name        string
		spec        *ocirepospecv1.Repository
		credentials map[string]string
		wantErr     bool
	}{
		{
			name: "add valid credentials",
			spec: &ocirepospecv1.Repository{
				BaseUrl: "https://example.com:443",
			},
			credentials: map[string]string{
				"username": "testuser",
				"password": "testpass",
			},
			wantErr: false,
		},
		{
			name: "add credentials with tokens",
			spec: &ocirepospecv1.Repository{
				BaseUrl: "https://example.com:443",
			},
			credentials: map[string]string{
				"refresh_token": "refreshtoken123",
				"access_token":  "accesstoken456",
			},
			wantErr: false,
		},
		{
			name: "invalid base URL",
			spec: &ocirepospecv1.Repository{
				BaseUrl: "://invalid-url",
			},
			credentials: map[string]string{
				"username": "testuser",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &credentialCache{}
			err := cache.add(tt.spec, tt.credentials)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Verify the credentials were added correctly
			if !tt.wantErr {
				url, err := runtime.ParseURLAndAllowNoScheme(tt.spec.BaseUrl)
				assert.NoError(t, err)
				host, port := url.Hostname(), url.Port()
				hostport := host + ":" + port
				got, err := cache.get(context.Background(), hostport)
				assert.NoError(t, err)
				assert.Equal(t, toCredential(tt.credentials), got)
			}
		})
	}
}

func TestCredentialCache_Overwrite(t *testing.T) {
	cache := &credentialCache{}
	spec := &ocirepospecv1.Repository{BaseUrl: "https://example.com:443"}

	// Add initial credentials
	initialCreds := map[string]string{
		"username": "user1",
		"password": "pass1",
	}
	err := cache.add(spec, initialCreds)
	require.NoError(t, err)

	// Add new credentials for the same identity
	newCreds := map[string]string{
		"username": "user2",
		"password": "pass2",
	}
	err = cache.add(spec, newCreds)
	require.NoError(t, err)

	// Verify the credentials were overwritten
	got, err := cache.get(context.Background(), "example.com:443")
	assert.NoError(t, err)
	assert.Equal(t, toCredential(newCreds), got)
}

func TestEqualCredentials(t *testing.T) {
	tests := []struct {
		name string
		a    auth.Credential
		b    auth.Credential
		want bool
	}{
		{
			name: "equal credentials",
			a: auth.Credential{
				Username:     "user",
				Password:     "pass",
				RefreshToken: "refresh",
				AccessToken:  "access",
			},
			b: auth.Credential{
				Username:     "user",
				Password:     "pass",
				RefreshToken: "refresh",
				AccessToken:  "access",
			},
			want: true,
		},
		{
			name: "different credentials",
			a: auth.Credential{
				Username: "user1",
				Password: "pass1",
			},
			b: auth.Credential{
				Username: "user2",
				Password: "pass2",
			},
			want: false,
		},
		{
			name: "empty credentials",
			a:    auth.EmptyCredential,
			b:    auth.EmptyCredential,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := equalCredentials(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}
