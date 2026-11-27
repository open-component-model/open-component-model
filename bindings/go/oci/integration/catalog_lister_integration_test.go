package integration_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/registry/remote"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/catalog"
	ocicreds "ocm.software/open-component-model/bindings/go/oci/credentials"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Test_Integration_OCIRepository_Lister lists OCM component names from a real
// containerized registry through the catalog ComponentLister, so the
// /v2/_catalog path is exercised end to end against distribution software. It
// pushes more components than one catalog page holds plus marker repositories
// before and after the component-descriptors block, so cursor pagination, the
// early stop and the prefix filter are all covered.
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

	creds := &ocicredsv1.OCICredentials{
		Type:     ocicredsv1.OCICredentialsVersionedType,
		Username: testUsername,
		Password: password,
	}

	// Push more components than a single catalog page (100) holds, so the scan
	// spans several pages.
	repoSpec := &ocirepospecv1.Repository{BaseUrl: registryAddress}
	repoProvider := provider.NewComponentVersionRepositoryProvider(provider.WithHTTPConfig(plainHTTPConfig()))
	repo, err := repoProvider.GetComponentVersionRepository(ctx, repoSpec, creds)
	r.NoError(err)

	const componentCount = 105
	expected := make([]string, 0, componentCount)
	for i := range componentCount {
		name := fmt.Sprintf("acme.org/app-%03d", i)
		addBarebonesComponentVersion(t, ctx, repo, name, "v1.0.0")
		expected = append(expected, name)
	}

	// Marker repositories that sort before and after the component-descriptors
	// block. Neither is an OCM component, so both must be filtered out: the
	// "aaa/" one is skipped by the start cursor and the "zzz/" one ends the
	// scan.
	authClient := createAuthClient(registryAddress, testUsername, password)
	hostAddress, err := registryContainer.HostAddress(ctx)
	r.NoError(err)
	reg, err := remote.NewRegistry(hostAddress)
	r.NoError(err)
	reg.PlainHTTP = true
	reg.Client = authClient
	pushMarkerRepo(t, ctx, reg, "aaa/marker")
	pushMarkerRepo(t, ctx, reg, "zzz/marker")

	parsed, err := ocmruntime.ParseURLAndAllowNoScheme(registryAddress)
	r.NoError(err)

	identity, err := identityv1.OCIRegistryIdentityFromOCIRepository(repoSpec)
	r.NoError(err)

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

// addBarebonesComponentVersion pushes a minimal component version. It is
// lighter than uploadDownloadBarebonesComponentVersion because it does not read
// the version back, which keeps the many-component loop fast.
func addBarebonesComponentVersion(t *testing.T, ctx context.Context, repo repository.ComponentVersionRepository, name, version string) {
	t.Helper()
	desc := descriptor.Descriptor{}
	desc.Component.Name = name
	desc.Component.Version = version
	desc.Component.Provider.Name = "ocm.software/open-component-model/bindings/go/oci/integration/test"
	require.NoError(t, repo.AddComponentVersion(ctx, &desc))
}

// pushMarkerRepo pushes an empty manifest to name:latest so the repository
// shows up in the registry catalog.
func pushMarkerRepo(t *testing.T, ctx context.Context, reg *remote.Registry, name string) {
	t.Helper()
	r := require.New(t)
	repo, err := reg.Repository(ctx, name)
	r.NoError(err)

	configBytes := []byte("{}")
	configDesc := ociImageSpecV1.Descriptor{
		MediaType: "application/vnd.oci.image.config.v1+json",
		Digest:    digest.FromBytes(configBytes),
		Size:      int64(len(configBytes)),
	}
	r.NoError(repo.Push(ctx, configDesc, bytes.NewReader(configBytes)))

	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    []ociImageSpecV1.Descriptor{},
	}
	manifestBytes, err := json.Marshal(manifest)
	r.NoError(err)
	manifestDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromBytes(manifestBytes),
		Size:      int64(len(manifestBytes)),
	}
	r.NoError(repo.Manifests().Push(ctx, manifestDesc, bytes.NewReader(manifestBytes)))
	r.NoError(repo.Tag(ctx, manifestDesc, "latest"))
}
