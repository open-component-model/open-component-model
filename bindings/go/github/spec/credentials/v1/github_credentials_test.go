package v1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestMustRegisterCredentialType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterCredentialType(scheme)

	t.Run("the versioned type resolves", func(t *testing.T) {
		obj, err := scheme.NewObject(runtime.NewVersionedType(GitHubCredentialsType, Version))
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

// The type must round-trip through JSON unchanged, so a credential config
// written by one process is read back identically by another.
func TestGitHubCredentials_JSONRoundTrip(t *testing.T) {
	creds := &GitHubCredentials{Type: runtime.NewVersionedType(GitHubCredentialsType, Version), Token: "ghp_secret"}

	data, err := json.Marshal(creds)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"GitHubCredentials/v1","token":"ghp_secret"}`, string(data))

	var decoded GitHubCredentials
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, creds, &decoded)

	again, err := json.Marshal(&decoded)
	require.NoError(t, err)
	assert.JSONEq(t, string(data), string(again))
}
