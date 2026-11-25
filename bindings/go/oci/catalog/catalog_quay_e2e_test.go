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

// TestQuayCataloger_E2E lists repositories of a real Quay namespace. It is
// env-gated and skips when OCM_E2E_QUAY_NAMESPACE is empty. A token can be set
// through OCM_E2E_QUAY_TOKEN to list private repositories; it is optional for
// public namespaces.
//
// The namespace is passed as the OCM subPath of an ociv1.Repository with the
// quay.io base URL, the same spec the builtin lister plugin resolves. The test
// cannot import the cli/internal builtin plugin from this package, so it builds
// the CatalogerOptions directly. It asserts no error and that every returned
// name is stripped correctly.
func TestQuayCataloger_E2E(t *testing.T) {
	namespace := os.Getenv("OCM_E2E_QUAY_NAMESPACE")
	if namespace == "" {
		t.Skip("OCM_E2E_QUAY_NAMESPACE is not set; skipping quay catalog e2e test")
	}
	token := os.Getenv("OCM_E2E_QUAY_TOKEN")

	spec := &ociv1.Repository{BaseUrl: "quay.io", SubPath: namespace}

	var creds *credsv1.OCICredentials
	if token != "" {
		creds = &credsv1.OCICredentials{
			Type:        credsv1.OCICredentialsVersionedType,
			AccessToken: token,
		}
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
