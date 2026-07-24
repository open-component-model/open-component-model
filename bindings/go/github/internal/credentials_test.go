package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCredentialConsumerIdentity(t *testing.T) {
	t.Run("resolves identity for a github repository URL", func(t *testing.T) {
		identity, err := CredentialConsumerIdentity("https://github.com/open-component-model/ocm")
		require.NoError(t, err)

		assert.Equal(t, "GitHubRepository", identity[runtime.IdentityAttributeType])
		assert.Equal(t, "github.com", identity[runtime.IdentityAttributeHostname])
	})

	t.Run("a scheme-less URL yields the same identity as its https form", func(t *testing.T) {
		withScheme, err := CredentialConsumerIdentity("https://github.com/open-component-model/ocm")
		require.NoError(t, err)
		withoutScheme, err := CredentialConsumerIdentity("github.com/open-component-model/ocm")
		require.NoError(t, err)

		assert.Equal(t, withScheme, withoutScheme,
			"the spec allows repoUrl with or without a scheme; both must resolve the same credentials")
		assert.Equal(t, "https", withoutScheme[runtime.IdentityAttributeScheme],
			"the scheme must be defaulted, not omitted")
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
