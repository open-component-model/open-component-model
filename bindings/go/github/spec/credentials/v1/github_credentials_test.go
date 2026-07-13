package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestMustRegisterCredentialType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	t.Run("the versioned type resolves", func(t *testing.T) {
		obj, err := scheme.NewObject(GitHubCredentialsVersionedType)
		require.NoError(t, err)
		assert.IsType(t, &GitHubCredentials{}, obj)
	})

	// The unversioned alias is what a .ocmconfig written as "GitHubCredentials"
	// (no /v1 suffix) resolves through.
	t.Run("the unversioned alias resolves", func(t *testing.T) {
		obj, err := scheme.NewObject(runtime.NewUnversionedType(GitHubCredentialsType))
		require.NoError(t, err)
		assert.IsType(t, &GitHubCredentials{}, obj)
	})
}

func TestGitHubCredentials_GetSetType(t *testing.T) {
	creds := &GitHubCredentials{Token: "ghp_secret"}
	creds.SetType(GitHubCredentialsVersionedType)
	assert.Equal(t, GitHubCredentialsVersionedType, creds.GetType())
}

func TestGitHubCredentials_DeepCopy(t *testing.T) {
	creds := &GitHubCredentials{Type: GitHubCredentialsVersionedType, Token: "ghp_secret"}

	clone, ok := creds.DeepCopyTyped().(*GitHubCredentials)
	require.True(t, ok)
	assert.Equal(t, creds, clone)

	clone.Token = "ghp_other"
	assert.Equal(t, "ghp_secret", creds.Token, "the copy must not alias the original")
}
