package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"oras.land/oras-go/v2/content"
	orasregistry "oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	ownershipv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/ownership/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Test_Integration_AssetToOwner uploads a by-value OCI resource through OCM
// to a live containerised registry and checks that the OCI Referrers API
// returns the expected ownership referrer (ADR 0016).
func Test_Integration_AssetToOwner(t *testing.T) {
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

	registryAddress, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	const subPath = "ocm/components"
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddress),
		urlresolver.WithSubPath(subPath),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(createAuthClient(registryAddress, testUsername, password)),
	)
	r.NoError(err)

	repoSpec := &ocirepospecv1.Repository{
		BaseUrl: "http://" + registryAddress,
		SubPath: subPath,
	}
	creds := &ocicredsv1.OCICredentials{
		Type:     ocicredsv1.OCICredentialsVersionedType,
		Username: testUsername,
		Password: password,
	}
	repo := newRepoFromConfig(t, ctx, repoSpec, creds, `
type: generic.config.ocm.software/v1
configurations:
  - type: ownership.config.ocm.software/v1alpha1
    policy: AddIfSupported
`)

	const (
		componentName    = "ocm.software/asset-to-owner-test"
		componentVersion = "v1.0.0"
		resourceName     = "backend-image"
	)

	t.Run("create component version and verify single ownership referrer", func(t *testing.T) {
		r := require.New(t)
		resourceDigest := uploadResource(t, ctx, repo, componentName, componentVersion, resourceName, []byte("ownership-payload"))

		referrers := listOwnershipReferrers(t, ctx, resolver, componentName, componentVersion, resourceDigest)
		r.Len(referrers, 1, "exactly one ownership referrer should be discoverable via the Referrers API")
		ref := referrers[0]

		// Referrer identifies the owning component by name and version.
		assert.Equal(t, componentName, ref.Annotations[annotations.OwnershipComponentName])
		assert.Equal(t, componentVersion, ref.Annotations[annotations.OwnershipComponentVersion])

		// Referrer identifies the artifact by identity and kind.
		var payload annotations.ArtifactOCIAnnotation
		require.NoError(t, json.Unmarshal([]byte(ref.Annotations[annotations.ArtifactAnnotationKey]), &payload))
		assert.Equal(t, annotations.ArtifactKindResource, payload.Kind)
		assert.Equal(t, resourceName, payload.Identity["name"])
		assert.Equal(t, componentVersion, payload.Identity["version"])
	})

	t.Run("multiple resources in a CV each get their own referrer", func(t *testing.T) {
		const (
			multiComponent = "ocm.software/asset-to-owner-multi-asset"
			backendName    = "backend-image"
			frontendName   = "frontend-image"
		)
		r := require.New(t)
		backendDigest := uploadResource(t, ctx, repo, multiComponent, componentVersion, backendName, []byte("backend-payload"))
		frontendDigest := uploadResource(t, ctx, repo, multiComponent, componentVersion, frontendName, []byte("frontend-payload"))
		r.NotEqual(backendDigest, frontendDigest, "distinct payloads must produce distinct subject digests")

		cases := []struct {
			label   string
			subject digest.Digest
			want    string
		}{
			{"backend", backendDigest, backendName},
			{"frontend", frontendDigest, frontendName},
		}
		for _, tc := range cases {
			t.Run(tc.label, func(t *testing.T) {
				referrers := listOwnershipReferrers(t, ctx, resolver, multiComponent, componentVersion, tc.subject)
				require.Len(t, referrers, 1, "exactly one referrer per asset")

				var payload annotations.ArtifactOCIAnnotation
				require.NoError(t, json.Unmarshal([]byte(referrers[0].Annotations[annotations.ArtifactAnnotationKey]), &payload))
				assert.Equal(t, tc.want, payload.Identity["name"],
					"%s referrer must point at its own asset, not the sibling", tc.label)
			})
		}
	})

	t.Run("re-uploading the same resource leaves a single referrer", func(t *testing.T) {
		var resourceDigest digest.Digest
		for i := range 3 {
			resourceDigest = uploadResource(t, ctx, repo, componentName, componentVersion, resourceName, []byte("ownership-payload"))
			require.NotEmptyf(t, resourceDigest, "re-upload attempt %d must yield a digest", i+1)
		}

		referrers := listOwnershipReferrers(t, ctx, resolver, componentName, componentVersion, resourceDigest)
		assert.Lenf(t, referrers, 1,
			"identical re-uploads must converge on a single referrer; got %d distinct manifests", len(referrers))
	})

	t.Run("ownership policy configuration", func(t *testing.T) {
		t.Run("global Never", func(t *testing.T) {
			neverRepo := newRepoFromConfig(t, ctx, repoSpec, creds, `
type: generic.config.ocm.software/v1
configurations:
  - type: ownership.config.ocm.software/v1alpha1
    policy: Never
`)

			const (
				neverComponent = "ocm.software/asset-to-owner-test-never"
				neverResource  = "backend-image-never"
			)
			resourceDigest := uploadResource(t, ctx, neverRepo, neverComponent, componentVersion, neverResource, []byte("ownership-payload-never"))

			referrers := listOwnershipReferrers(t, ctx, resolver, neverComponent, componentVersion, resourceDigest)
			assert.Emptyf(t, referrers,
				"config-driven Never must not push any ownership referrer; found %d", len(referrers))
		})

		t.Run("global Never, per-repo AddIfSupported", func(t *testing.T) {
			// Per-repo override opts one repo in even when the global default is Never.
			overrideRepo := newRepoFromConfig(t, ctx, repoSpec, creds, fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
  - type: ownership.config.ocm.software/v1alpha1
    policy: Never
    repositories:
      - repository:
          type: OCIRepository/v1
          baseUrl: %s
          subPath: %s
        policy: AddIfSupported
`, repoSpec.BaseUrl, repoSpec.SubPath))

			const (
				overrideComponent = "ocm.software/asset-to-owner-test-override-add-if-supported"
				overrideResource  = "backend-image-override-add-if-supported"
			)
			resourceDigest := uploadResource(t, ctx, overrideRepo, overrideComponent, componentVersion, overrideResource, []byte("ownership-payload-override-add-if-supported"))

			referrers := listOwnershipReferrers(t, ctx, resolver, overrideComponent, componentVersion, resourceDigest)
			assert.Lenf(t, referrers, 1,
				"per-repo AddIfSupported override must push an ownership referrer even when global is Never; found %d", len(referrers))
		})

		t.Run("global AddIfSupported, per-repo Never", func(t *testing.T) {
			// Per-repo Never override must beat the top-level AddIfSupported fallback.
			overrideRepo := newRepoFromConfig(t, ctx, repoSpec, creds, fmt.Sprintf(`
type: generic.config.ocm.software/v1
configurations:
  - type: ownership.config.ocm.software/v1alpha1
    policy: AddIfSupported
    repositories:
      - repository:
          type: OCIRepository/v1
          baseUrl: %s
          subPath: %s
        policy: Never
`, repoSpec.BaseUrl, repoSpec.SubPath))

			const (
				overrideComponent = "ocm.software/asset-to-owner-test-override-never"
				overrideResource  = "backend-image-override-never"
			)
			resourceDigest := uploadResource(t, ctx, overrideRepo, overrideComponent, componentVersion, overrideResource, []byte("ownership-payload-override-never"))

			referrers := listOwnershipReferrers(t, ctx, resolver, overrideComponent, componentVersion, resourceDigest)
			assert.Emptyf(t, referrers,
				"per-repo Never override must suppress ownership referrer even when global is AddIfSupported; found %d", len(referrers))
		})
	})
}

// newRepoFromConfig constructs an OCI component-version repository through
// the provider, parsing the given OCM YAML config — the same shape a user
// would write to `.ocmconfig`.
func newRepoFromConfig(
	t *testing.T,
	ctx context.Context,
	repoSpec *ocirepospecv1.Repository,
	creds *ocicredsv1.OCICredentials,
	ocmConfigYAML string,
) *oci.Repository {
	t.Helper()
	r := require.New(t)
	var cfg genericv1.Config
	r.NoError(genericv1.Scheme.Decode(strings.NewReader(ocmConfigYAML), &cfg))
	ownershipConfig, err := ownershipv1alpha1.Lookup(&cfg)
	r.NoError(err)
	prov := provider.NewComponentVersionRepositoryProvider(
		provider.WithUserAgent(userAgent),
		provider.WithTempDir(t.TempDir()),
		provider.WithOwnershipConfig(ownershipConfig),
	)
	cvRepo, err := prov.GetComponentVersionRepository(ctx, repoSpec, creds)
	r.NoError(err)
	repo, ok := cvRepo.(*oci.Repository)
	r.Truef(ok, "expected *oci.Repository, got %T", cvRepo)
	return repo
}

// uploadResource pushes a one-layer OCI image as a local resource through repo
// and returns the digest of the resulting subject manifest.
func uploadResource(t *testing.T, ctx context.Context, repo *oci.Repository, component, version, name string, payload []byte) digest.Digest {
	t.Helper()
	r := require.New(t)
	data, _ := createSingleLayerOCIImage(t, payload)
	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: name, Version: version},
		},
		Type:     "ociArtifact",
		Relation: descriptor.LocalRelation,
		Access: &v2.LocalBlob{
			Type: ocmruntime.Type{
				Name:    v2.LocalBlobAccessType,
				Version: v2.LocalBlobAccessTypeVersion,
			},
			MediaType:      layout.MediaTypeOCIImageLayoutTarGzipV1,
			LocalReference: digest.FromBytes(data).String(),
		},
	}
	newRes, err := repo.AddLocalResource(ctx, component, version, res, inmemory.New(bytes.NewReader(data)))
	r.NoError(err)
	var localAccess v2.LocalBlob
	r.NoError(v2.Scheme.Convert(newRes.Access, &localAccess))
	return digest.Digest(localAccess.LocalReference)
}

// listOwnershipReferrers walks the OCI Referrers API for subjectDigest and
// returns every referrer carrying [annotations.OwnershipArtifactType].
func listOwnershipReferrers(t *testing.T, ctx context.Context, resolver *urlresolver.CachingResolver, component, version string, subjectDigest digest.Digest) []ociImageSpecV1.Descriptor {
	t.Helper()
	r := require.New(t)
	compRef := resolver.ComponentVersionReference(ctx, component, version)
	store, err := resolver.StoreForReference(ctx, compRef)
	r.NoError(err)
	graphStore, ok := store.(content.ReadOnlyGraphStorage)
	r.Truef(ok, "store %T must implement content.ReadOnlyGraphStorage for referrers discovery", store)
	subject, err := store.Resolve(ctx, subjectDigest.String())
	r.NoError(err)
	refs, err := orasregistry.Referrers(ctx, graphStore, subject, annotations.OwnershipArtifactType)
	r.NoError(err)
	return refs
}
