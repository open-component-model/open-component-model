package internal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/maven/internal"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCredentialConsumerIdentity(t *testing.T) {
	t.Run("http repo", func(t *testing.T) {
		id, err := internal.CredentialConsumerIdentity("https://maven.example.com/repo")
		require.NoError(t, err)
		assert.Equal(t, "maven.example.com", id[runtime.IdentityAttributeHostname])
		assert.Equal(t, "https", id[runtime.IdentityAttributeScheme])
		assert.Equal(t, "MavenRepository", id[runtime.IdentityAttributeType])
	})
	t.Run("empty repo errors", func(t *testing.T) {
		_, err := internal.CredentialConsumerIdentity("")
		require.Error(t, err)
	})
}
