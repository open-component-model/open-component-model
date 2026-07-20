package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	directcredsv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func directCredentials(properties map[string]string) *directcredsv1.DirectCredentials {
	return &directcredsv1.DirectCredentials{
		Type:       runtime.NewVersionedType(directcredsv1.CredentialsType, directcredsv1.Version),
		Properties: properties,
	}
}

func TestConvertToGitHubCredentials(t *testing.T) {
	t.Run("nil credentials convert to nil without an error", func(t *testing.T) {
		converted, err := ConvertToGitHubCredentials(nil)
		require.NoError(t, err)
		assert.Nil(t, converted, "the github access is usable anonymously, so absent credentials are not an error")
	})

	t.Run("credentials with an empty type convert to nil without an error", func(t *testing.T) {
		converted, err := ConvertToGitHubCredentials(&GitHubCredentials{})
		require.NoError(t, err)
		assert.Nil(t, converted)
	})

	t.Run("typed github credentials pass through", func(t *testing.T) {
		converted, err := ConvertToGitHubCredentials(&GitHubCredentials{
			Type:  runtime.NewVersionedType(GitHubCredentialsType, Version),
			Token: "ghp_secret",
		})
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Equal(t, "ghp_secret", converted.Token)
	})

	t.Run("direct credentials map the token property", func(t *testing.T) {
		converted, err := ConvertToGitHubCredentials(directCredentials(map[string]string{"token": "ghp_secret"}))
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Equal(t, "ghp_secret", converted.Token)
		assert.Equal(t, runtime.NewVersionedType(GitHubCredentialsType, Version), converted.Type)
	})

	// Rejecting an unusable credential is the consumer's call, not the converter's.
	t.Run("direct credentials without a token convert to an empty token", func(t *testing.T) {
		converted, err := ConvertToGitHubCredentials(directCredentials(map[string]string{"username": "octocat"}))
		require.NoError(t, err)
		require.NotNil(t, converted)
		assert.Empty(t, converted.Token)
	})

	t.Run("an unregistered credential type is rejected", func(t *testing.T) {
		_, err := ConvertToGitHubCredentials(&runtime.Raw{
			Type: runtime.NewVersionedType("SomeOtherCredentials", "v1"),
			Data: []byte(`{"type":"SomeOtherCredentials/v1"}`),
		})
		assert.ErrorContains(t, err, "SomeOtherCredentials")
	})
}
