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
	t.Run("port and path are part of the identity", func(t *testing.T) {
		id, err := internal.CredentialConsumerIdentity("https://nexus.example.com:8081/repository/maven-releases")
		require.NoError(t, err)
		assert.Equal(t, "nexus.example.com", id[runtime.IdentityAttributeHostname])
		assert.Equal(t, "8081", id[runtime.IdentityAttributePort])
		assert.Equal(t, "repository/maven-releases", id[runtime.IdentityAttributePath])
	})
	t.Run("unparsable repo errors", func(t *testing.T) {
		_, err := internal.CredentialConsumerIdentity("://bad")
		require.ErrorContains(t, err, "error parsing maven repository URL")
	})
	t.Run("empty repo errors", func(t *testing.T) {
		_, err := internal.CredentialConsumerIdentity("")
		require.Error(t, err)
	})
}
