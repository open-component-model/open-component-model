package examples

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestExample_StaticCredentialResolver demonstrates creating a static
// credential resolver and resolving credentials by identity.
func TestExample_StaticCredentialResolver(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	// Define a credential map keyed by identity attributes.
	credMap := map[string]map[string]string{
		"hostname=registry.example.com,type=OCIRegistry": {
			"username": "admin",
			"password": "s3cret",
		},
	}

	resolver := credentials.NewStaticCredentialsResolver(credMap)

	// Resolve credentials for a matching identity.
	creds, err := resolver.Resolve(ctx, runtime.Identity{
		"type":     "OCIRegistry",
		"hostname": "registry.example.com",
	})
	r.NoError(err)
	r.Equal("admin", creds["username"])
	r.Equal("s3cret", creds["password"])
}

// TestExample_CredentialResolutionNotFound shows how credential resolution
// behaves when no matching credentials exist.
func TestExample_CredentialResolutionNotFound(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	resolver := credentials.NewStaticCredentialsResolver(map[string]map[string]string{})

	_, err := resolver.Resolve(ctx, runtime.Identity{
		"type":     "OCIRegistry",
		"hostname": "unknown.registry.io",
	})

	r.Error(err)
	r.ErrorIs(err, credentials.ErrNotFound)
}
