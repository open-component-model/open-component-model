// Step 3: Credential Resolution
//
// What you'll learn:
//   - Creating a static credential resolver
//   - Resolving credentials by identity (hostname, type)
//   - Handling the case when no credentials match
//
// When working with remote repositories (like OCI registries), you need
// credentials. OCM's credential system resolves credentials by matching
// identity attributes — the same identity model used for resources and
// components. This step shows the basics; Step 6 puts credentials to use
// with a real OCI registry.

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
			"username": "test-user",
			"password": "test-password",
		},
	}

	resolver := credentials.NewStaticCredentialsResolver(credMap)

	// Resolve credentials for a matching identity.
	creds, err := resolver.Resolve(ctx, runtime.Identity{
		"type":     "OCIRegistry",
		"hostname": "registry.example.com",
	})
	r.NoError(err)
	r.Equal("test-user", creds["username"])
	r.Equal("test-password", creds["password"])
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
