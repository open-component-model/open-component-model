package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"

	"ocm.software/open-component-model/bindings/go/oci/catalog"
	ocicreds "ocm.software/open-component-model/bindings/go/oci/credentials"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Test_Integration_OCIRepository_Lister lists OCM component names from a real
// containerized registry through the catalog ComponentLister, so the
// /v2/_catalog path is exercised end to end against distribution software.
func Test_Integration_OCIRepository_Lister(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	password := generateRandomPassword(t, passwordLength)
	htpasswd := generateHtpasswd(t, testUsername, password)

	t.Logf("Launching test registry (%s)...", distributionRegistryImage)
	registryContainer, err := registry.Run(ctx, distributionRegistryImage,
		registry.WithHtpasswd(htpasswd),
		testcontainers.WithEnv(map[string]string{
			"REGISTRY_VALIDATION_DISABLED": "true",
			"REGISTRY_LOG_LEVEL":           "debug",
		}),
		testcontainers.WithLogger(log.TestLogger(t)),
	)
	r := require.New(t)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(testcontainers.TerminateContainer(registryContainer))
	})

	registryAddress, err := registryContainer.Address(ctx)
	r.NoError(err)

	// Push a few component versions. The provider writes them under
	// component-descriptors/<name>, which is what the catalog lister matches.
	repoSpec := &ocirepospecv1.Repository{BaseUrl: registryAddress}
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithHTTPConfig(plainHTTPConfig()))
	repo, err := repoProvider.GetComponentVersionRepository(ctx, repoSpec, &ocicredsv1.OCICredentials{
		Type:     ocicredsv1.OCICredentialsVersionedType,
		Username: testUsername,
		Password: password,
	})
	r.NoError(err)

	components := []struct{ name, version string }{
		{"github.com/acme.org/helloworld", "v1.0.0"},
		{"github.com/acme.org/helloworld", "v2.0.0"},
		{"github.com/acme.org/helloocm", "v1.0.0"},
		{"github.com/acme.org/hello-open-component-model", "v1.0.0"},
	}
	for _, c := range components {
		uploadDownloadBarebonesComponentVersion(t, repo, c.name, c.version)
	}

	// Expectation: sorted, unique component names.
	expected := []string{
		"github.com/acme.org/hello-open-component-model",
		"github.com/acme.org/helloocm",
		"github.com/acme.org/helloworld",
	}

	parsed, err := ocmruntime.ParseURLAndAllowNoScheme(registryAddress)
	r.NoError(err)

	identity, err := identityv1.OCIRegistryIdentityFromOCIRepository(repoSpec)
	r.NoError(err)

	creds := &ocicredsv1.OCICredentials{
		Type:     ocicredsv1.OCICredentialsVersionedType,
		Username: testUsername,
		Password: password,
	}

	cataloger, err := catalog.NewDistributionCataloger(catalog.CatalogerOptions{
		Host:       parsed.Host,
		HTTPClient: http.DefaultClient,
		AuthClient: ocicreds.AuthClient(http.DefaultClient, identity, creds, userAgent),
		PlainHTTP:  true,
	})
	r.NoError(err)

	lister := catalog.NewComponentLister(cataloger, "")

	var names []string
	r.NoError(lister.ListComponents(ctx, "", func(found []string) error {
		names = append(names, found...)
		return nil
	}))

	r.Equal(expected, names)
}
