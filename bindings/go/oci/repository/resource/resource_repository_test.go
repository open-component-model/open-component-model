package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/auth"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

func TestCreateRepositoryWithFilesystemConfig(t *testing.T) {
	r := require.New(t)

	tests := []struct {
		name             string
		filesystemConfig *filesystemv1alpha1.Config
		expectError      bool
	}{
		{
			name: "with filesystem config",
			filesystemConfig: &filesystemv1alpha1.Config{
				TempFolder: "/tmp/test",
			},
			expectError: false,
		},
		{
			name:        "without filesystem config",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &ociv1.Repository{
				BaseUrl: "localhost:5000",
			}
			credentials := map[string]string{}

			repo, err := createRepository(spec, credentials, tt.filesystemConfig, "test")

			if tt.expectError {
				r.Error(err, "expected error")
				r.Nil(repo, "repository should be nil")
			} else {
				r.NoError(err, "should not error")
				r.NotNil(repo, "repository should not be nil")
			}
		})
	}
}

func TestClientCredentials(t *testing.T) {
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
				ocicredentials.CredentialKeyUsername:     "user",
				ocicredentials.CredentialKeyPassword:     "pass",
				ocicredentials.CredentialKeyAccessToken:  "atoken",
				ocicredentials.CredentialKeyRefreshToken: "rtoken",
			},
			expected: auth.Credential{
				Username:     "user",
				Password:     "pass",
				AccessToken:  "atoken",
				RefreshToken: "rtoken",
			},
		},
		{
			name: "legacy snake_case keys",
			credentials: map[string]string{
				"username":      "user",
				"password":      "pass",
				ocicredentials.LegacyCredentialKeyAccessToken:  "atoken",
				ocicredentials.LegacyCredentialKeyRefreshToken: "rtoken",
			},
			expected: auth.Credential{
				Username:     "user",
				Password:     "pass",
				AccessToken:  "atoken",
				RefreshToken: "rtoken",
			},
		},
		{
			name: "camelCase takes precedence over snake_case",
			credentials: map[string]string{
				ocicredentials.CredentialKeyAccessToken:  "camel",
				ocicredentials.LegacyCredentialKeyAccessToken:                          "snake",
				ocicredentials.CredentialKeyRefreshToken: "camel-refresh",
				ocicredentials.LegacyCredentialKeyRefreshToken:                         "snake-refresh",
			},
			expected: auth.Credential{
				AccessToken:  "camel",
				RefreshToken: "camel-refresh",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := clientCredentials(tt.credentials)
			assert.Equal(t, tt.expected, result)
		})
	}
}
