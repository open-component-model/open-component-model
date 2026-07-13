package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
)

// Scheme is populated in init, so nothing else proves it holds what it advertises.
func TestSchemeRegistersGitHubCredentials(t *testing.T) {
	obj, err := Scheme.NewObject(v1.GitHubCredentialsVersionedType)
	require.NoError(t, err)
	assert.IsType(t, &v1.GitHubCredentials{}, obj)
}
