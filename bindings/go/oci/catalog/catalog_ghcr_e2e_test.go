//go:build e2e

package catalog

import (
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
)

// TestGHCRCataloger_E2E lists the container packages of a real GitHub owner.
// It is env-gated and skips when OCM_E2E_GHCR_OWNER is empty or when no token
// is set through OCM_E2E_GHCR_TOKEN or GITHUB_TOKEN.
//
// The owner is passed as the OCM subPath of an ociv1.Repository with the
// ghcr.io base URL, the same spec the builtin lister plugin resolves. The test
// cannot import the cli/internal builtin plugin from this package, so it builds
// the CatalogerOptions directly. It asserts no error and that every returned
// name is stripped correctly.
func TestGHCRCataloger_E2E(t *testing.T) {
	owner := os.Getenv("OCM_E2E_GHCR_OWNER")
	if owner == "" {
		t.Skip("OCM_E2E_GHCR_OWNER is not set; skipping ghcr catalog e2e test")
	}
	token := os.Getenv("OCM_E2E_GHCR_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		t.Skip("OCM_E2E_GHCR_TOKEN and GITHUB_TOKEN are not set; skipping ghcr catalog e2e test")
	}

	spec := &ociv1.Repository{BaseUrl: "ghcr.io", SubPath: owner}

	creds := &credsv1.OCICredentials{
		Type:        credsv1.OCICredentialsVersionedType,
		AccessToken: token,
	}

	cataloger, err := CatalogerFor(CatalogerOptions{
		Host:        spec.BaseUrl,
		SubPath:     spec.SubPath,
		HTTPClient:  http.DefaultClient,
		Credentials: creds,
	})
	require.NoError(t, err)

	lister := NewComponentLister(cataloger, spec.SubPath)

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
