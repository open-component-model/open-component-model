package internal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/pypi/internal"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestCredentialConsumerIdentity(t *testing.T) {
	t.Run("https index", func(t *testing.T) {
		id, err := internal.CredentialConsumerIdentity("https://pypi.org/simple")
		require.NoError(t, err)
		assert.Equal(t, "pypi.org", id[runtime.IdentityAttributeHostname])
		assert.Equal(t, "https", id[runtime.IdentityAttributeScheme])
		assert.Equal(t, "PyPIRepository", id[runtime.IdentityAttributeType])
	})
	t.Run("port and path are part of the identity", func(t *testing.T) {
		id, err := internal.CredentialConsumerIdentity("https://nexus.example.com:8081/repository/pypi-hosted")
		require.NoError(t, err)
		assert.Equal(t, "nexus.example.com", id[runtime.IdentityAttributeHostname])
		assert.Equal(t, "8081", id[runtime.IdentityAttributePort])
		assert.Equal(t, "repository/pypi-hosted", id[runtime.IdentityAttributePath])
	})
	t.Run("unparsable index errors", func(t *testing.T) {
		_, err := internal.CredentialConsumerIdentity("://bad")
		require.ErrorContains(t, err, "error parsing pypi index URL")
	})
	t.Run("empty index errors", func(t *testing.T) {
		_, err := internal.CredentialConsumerIdentity("")
		require.Error(t, err)
	})
}
