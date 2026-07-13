package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	credsv1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCredentialConsumerIdentity(t *testing.T) {
	t.Run("resolves identity for a github repository URL", func(t *testing.T) {
		identity, err := CredentialConsumerIdentity("https://github.com/open-component-model/ocm")
		require.NoError(t, err)

		assert.Equal(t, "GitHubRepository", identity[runtime.IdentityAttributeType])
		assert.Equal(t, "github.com", identity[runtime.IdentityAttributeHostname])
	})

	t.Run("resolves identity for a github enterprise URL", func(t *testing.T) {
		identity, err := CredentialConsumerIdentity("https://github.enterprise.example/org/repo")
		require.NoError(t, err)

		assert.Equal(t, "GitHubRepository", identity[runtime.IdentityAttributeType])
		assert.Equal(t, "github.enterprise.example", identity[runtime.IdentityAttributeHostname])
	})

	t.Run("fails for an empty repository URL", func(t *testing.T) {
		_, err := CredentialConsumerIdentity("")
		assert.ErrorContains(t, err, "repository")
	})
}

func directCredentials(props map[string]string) *credv1.DirectCredentials {
	return &credv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credv1.CredentialsType, credv1.Version),
		Properties: props,
	}
}

func TestCredentialsFrom(t *testing.T) {
	t.Run("nil credentials mean anonymous access", func(t *testing.T) {
		credentials, err := CredentialsFrom(nil)
		require.NoError(t, err)
		assert.Nil(t, credentials, "anonymous access is signalled by nil credentials, not an empty token")
	})

	t.Run("token property is used", func(t *testing.T) {
		credentials, err := CredentialsFrom(directCredentials(map[string]string{"token": "ghp_secret"}))
		require.NoError(t, err)
		require.NotNil(t, credentials)
		assert.Equal(t, "ghp_secret", credentials.Token)
	})

	t.Run("accessToken is accepted as an alias", func(t *testing.T) {
		credentials, err := CredentialsFrom(directCredentials(map[string]string{"accessToken": "ghp_secret"}))
		require.NoError(t, err)
		require.NotNil(t, credentials)
		assert.Equal(t, "ghp_secret", credentials.Token)
	})

	t.Run("a typed github credential passes through", func(t *testing.T) {
		credentials, err := CredentialsFrom(&credsv1.GitHubCredentials{
			Type:  credsv1.GitHubCredentialsVersionedType,
			Token: "ghp_secret",
		})
		require.NoError(t, err)
		require.NotNil(t, credentials)
		assert.Equal(t, "ghp_secret", credentials.Token)
	})

	t.Run("present but unusable credentials are rejected", func(t *testing.T) {
		_, err := CredentialsFrom(directCredentials(map[string]string{"username": "octocat"}))
		assert.ErrorContains(t, err, "token")
	})
}
