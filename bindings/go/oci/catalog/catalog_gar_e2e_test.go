//go:build e2e

package catalog

import (
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestGARCataloger_E2E lists components from a real Google Artifact Registry
// or legacy GCR registry. It is env-gated and skips when OCM_E2E_GAR_REGISTRY
// is empty. Authentication uses Application Default Credentials from the
// environment (for example a service account key referenced by
// GOOGLE_APPLICATION_CREDENTIALS, or a gcloud login).
//
// OCM_E2E_GAR_REGISTRY is the registry host, for example
// europe-docker.pkg.dev. OCM_E2E_GAR_SUBPATH is the OCM repository prefix,
// typically <project>/<repository> for Artifact Registry.
//
// The test builds the same CatalogerOptions the builtin lister plugin builds
// because the oci/catalog package cannot import the cli/internal builtin
// plugin. It asserts no error and that every returned name is stripped
// correctly.
func TestGARCataloger_E2E(t *testing.T) {
	host := os.Getenv("OCM_E2E_GAR_REGISTRY")
	if host == "" {
		t.Skip("OCM_E2E_GAR_REGISTRY is not set; skipping gar catalog e2e test")
	}
	subPath := os.Getenv("OCM_E2E_GAR_SUBPATH")

	parsed, err := runtime.ParseURLAndAllowNoScheme(host)
	require.NoError(t, err)

	cataloger, err := CatalogerFor(CatalogerOptions{
		Host:       parsed.Host,
		SubPath:    subPath,
		HTTPClient: http.DefaultClient,
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
