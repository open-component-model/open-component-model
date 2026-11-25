//go:build e2e

package catalog

import (
	"net/http"
	"os"
	"testing"

	"github.com/oras-project/oras-go/v3/registry/remote/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ocicreds "ocm.software/open-component-model/bindings/go/oci/credentials"
	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestACRCataloger_E2E lists components from a real Azure Container Registry.
// It is env-gated and skips when OCM_E2E_ACR_REGISTRY is empty. Credentials
// can be set through OCM_E2E_ACR_USERNAME and OCM_E2E_ACR_PASSWORD (or a token
// as the password) to list private repositories; they are optional for
// anonymous access.
//
// The test builds the same CatalogerOptions the builtin lister plugin builds
// (identity, auth client, cataloger) because the oci/catalog package cannot
// import the cli/internal builtin plugin. It asserts no error and that every
// returned name is stripped correctly.
func TestACRCataloger_E2E(t *testing.T) {
	host := os.Getenv("OCM_E2E_ACR_REGISTRY")
	if host == "" {
		t.Skip("OCM_E2E_ACR_REGISTRY is not set; skipping acr catalog e2e test")
	}
	subPath := os.Getenv("OCM_E2E_ACR_SUBPATH")
	username := os.Getenv("OCM_E2E_ACR_USERNAME")
	password := os.Getenv("OCM_E2E_ACR_PASSWORD")

	spec := &ociv1.Repository{BaseUrl: host, SubPath: subPath}

	identity, err := identityv1.OCIRegistryIdentityFromOCIRepository(spec)
	require.NoError(t, err)

	parsed, err := runtime.ParseURLAndAllowNoScheme(host)
	require.NoError(t, err)

	var creds *credsv1.OCICredentials
	if username != "" || password != "" {
		creds = &credsv1.OCICredentials{
			Type:     credsv1.OCICredentialsVersionedType,
			Username: username,
			Password: password,
		}
	}

	header := http.Header{"User-Agent": {"ocm-catalog-e2e"}}
	authClient := &auth.Client{
		Client:         http.DefaultClient,
		Cache:          auth.NewCache(),
		CredentialFunc: ocicreds.CredentialFunc(identity, creds),
		Header:         header,
		TokenFetcher:   auth.NewCompositeTokenFetcher(http.DefaultClient, header, "", true),
	}

	consumerIdentity, err := identityv1.IdentityFromOCIRepository(spec)
	require.NoError(t, err)

	cataloger, err := CatalogerFor(CatalogerOptions{
		Host:        parsed.Host,
		SubPath:     subPath,
		HTTPClient:  http.DefaultClient,
		AuthClient:  authClient,
		Credentials: creds,
		Identity:    runtime.Identity(consumerIdentity),
	})
	require.NoError(t, err)

	lister := NewComponentLister(cataloger, subPath)

	var names []string
	err = lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	})
	require.NoError(t, err)

	for _, name := range names {
		assert.NotContains(t, name, "component-descriptors/", "name %q should not carry the component-descriptors prefix", name)
	}
}
