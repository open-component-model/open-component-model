//go:build e2e

package catalog

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestECRCataloger_E2E lists components from a real private Amazon ECR
// registry. It is env-gated and skips when OCM_E2E_ECR_REGISTRY is empty.
// Credentials come from the standard AWS environment (AWS_REGION,
// AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, or a shared profile) through the
// default AWS credential chain.
//
// OCM_E2E_ECR_REGISTRY is the full ECR host, for example
// 123456789012.dkr.ecr.eu-central-1.amazonaws.com. OCM_E2E_ECR_SUBPATH is an
// optional OCM repository prefix.
//
// The test builds the same CatalogerOptions the builtin lister plugin builds
// because the oci/catalog package cannot import the cli/internal builtin
// plugin. It asserts no error and that every returned name is stripped
// correctly.
func TestECRCataloger_E2E(t *testing.T) {
	host := os.Getenv("OCM_E2E_ECR_REGISTRY")
	if host == "" {
		t.Skip("OCM_E2E_ECR_REGISTRY is not set; skipping ecr catalog e2e test")
	}
	subPath := os.Getenv("OCM_E2E_ECR_SUBPATH")

	parsed, err := runtime.ParseURLAndAllowNoScheme(host)
	require.NoError(t, err)

	cataloger, err := CatalogerFor(CatalogerOptions{
		Host:    parsed.Host,
		SubPath: subPath,
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
