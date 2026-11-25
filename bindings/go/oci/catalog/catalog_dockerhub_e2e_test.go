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

// TestDockerHubCataloger_E2E lists repositories of a real Docker Hub namespace.
// It is env-gated and skips when OCM_E2E_DOCKERHUB_NAMESPACE is empty.
//
// The namespace is passed as the OCM subPath of an ociv1.Repository with the
// docker.io base URL, the same spec the builtin lister plugin resolves. The
// test cannot import the cli/internal builtin plugin from this package, so it
// builds the CatalogerOptions directly. It asserts no error and that every
// returned name is stripped correctly.
func TestDockerHubCataloger_E2E(t *testing.T) {
	namespace := os.Getenv("OCM_E2E_DOCKERHUB_NAMESPACE")
	if namespace == "" {
		t.Skip("OCM_E2E_DOCKERHUB_NAMESPACE is not set; skipping docker hub catalog e2e test")
	}
	username := os.Getenv("OCM_E2E_DOCKERHUB_USERNAME")
	password := os.Getenv("OCM_E2E_DOCKERHUB_PASSWORD")

	spec := &ociv1.Repository{BaseUrl: "docker.io", SubPath: namespace}

	var creds *credsv1.OCICredentials
	if username != "" || password != "" {
		creds = &credsv1.OCICredentials{
			Type:     credsv1.OCICredentialsVersionedType,
			Username: username,
			Password: password,
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
