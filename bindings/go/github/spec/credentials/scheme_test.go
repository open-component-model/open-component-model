package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/github/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Scheme is populated in init, so nothing else proves it holds what it advertises.
func TestSchemeRegistersGitHubCredentials(t *testing.T) {
	obj, err := Scheme.NewObject(runtime.NewVersionedType(v1.GitHubCredentialsType, v1.Version))
	require.NoError(t, err)
	assert.IsType(t, &v1.GitHubCredentials{}, obj)
}
