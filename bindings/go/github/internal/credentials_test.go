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
