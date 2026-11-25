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

// TestDistributionCataloger_E2E lists components from a real registry. It is
// env-gated and skips when OCM_E2E_CATALOG_URL is empty.
//
// The test builds the same CatalogerOptions the builtin lister plugin builds
// (identity, auth client, cataloger) because the oci/catalog package cannot
// import the cli/internal builtin plugin. It asserts no error and that every
// returned name is stripped correctly (no component-descriptors/ prefix and no
// subPath prefix).
func TestDistributionCataloger_E2E(t *testing.T) {
	host := os.Getenv("OCM_E2E_CATALOG_URL")
	if host == "" {
		t.Skip("OCM_E2E_CATALOG_URL is not set; skipping registry catalog e2e test")
	}
	subPath := os.Getenv("OCM_E2E_CATALOG_SUBPATH")
	username := os.Getenv("OCM_E2E_CATALOG_USERNAME")
	password := os.Getenv("OCM_E2E_CATALOG_PASSWORD")
	// OCM_E2E_CATALOG_PLAIN_HTTP lets the test reach a registry over cleartext
	// HTTP, for example a local registry:2 or zot instance.
	plainHTTP := os.Getenv("OCM_E2E_CATALOG_PLAIN_HTTP") == "true"

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

	cataloger, err := NewDistributionCataloger(CatalogerOptions{
		Host:       parsed.Host,
		HTTPClient: http.DefaultClient,
		AuthClient: authClient,
		PlainHTTP:  plainHTTP,
	})
	require.NoError(t, err)

	lister := NewComponentLister(cataloger, subPath)

	var names []string
	err = lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	})
	require.NoError(t, err)

	t.Logf("registry %q returned %d component name(s): %v", parsed.Host, len(names), names)

	for _, name := range names {
		assert.NotContains(t, name, "component-descriptors/", "name %q should not carry the component-descriptors prefix", name)
	}
}
