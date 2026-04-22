package integration_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"oras.land/oras-go/v2/content"
	orasregistry "oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Test_Integration_AssetToOwner verifies the asset-to-owner scenario
// end-to-end (ADR 0015): a by-value OCI resource uploaded through the OCM
// OCI binding must be discoverable as an ownership referrer via the OCI
// Distribution Referrers API.
//
// Verification goes through the ORAS Go SDK (`registry.Referrers`,
// `store.Fetch`) against a live containerised registry — the same API path
// that every OCI v1.1 client uses under the covers.
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

	client := createAuthClient(registryAddress, testUsername, password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(client),
	)
	r.NoError(err)

	repo, err := oci.NewRepository(
		oci.WithResolver(resolver),
		oci.WithTempDir(t.TempDir()),
		oci.WithOwnershipReferrerPolicy(oci.OwnershipReferrerPolicyEnabled),
	)
	r.NoError(err)

	const (
		componentName    = "ocm.software/asset-to-owner-test"
		componentVersion = "v1.0.0"
		resourceName     = "backend-image"
	)

	// Shared state between the two nested sub-tests: step 1 populates,
	// step 2 reads. Using local vars keeps the dependency explicit.
	var resourceDigest digest.Digest

	t.Run("create component version directly in OCI", func(t *testing.T) {
		r := require.New(t)

		data, _ := createSingleLayerOCIImage(t, []byte("ownership-payload"))

		res := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: resourceName, Version: componentVersion},
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

		newRes, err := repo.AddLocalResource(ctx, componentName, componentVersion, res, inmemory.New(bytes.NewReader(data)))
		r.NoError(err)

		var localAccess v2.LocalBlob
		r.NoError(v2.Scheme.Convert(newRes.Access, &localAccess))
		resourceDigest = digest.Digest(localAccess.LocalReference)
	})

	t.Run("verify ownership referrer", func(t *testing.T) {
		r := require.New(t)
		r.NotEmpty(resourceDigest, "step 1 must have set resourceDigest")

		// Resolve the component-version repo store (a *remote.Repository
		// under the hood). It implements both content.ReadOnlyGraphStorage
		// and registry.ReferrerLister, which is what registry.Referrers
		// needs to negotiate Referrers API vs tag fallback transparently.
		compRef := resolver.ComponentVersionReference(ctx, componentName, componentVersion)
		store, err := resolver.StoreForReference(ctx, compRef)
		r.NoError(err)

		graphStore, ok := store.(content.ReadOnlyGraphStorage)
		r.Truef(ok, "store %T must implement content.ReadOnlyGraphStorage for referrers discovery", store)

		subject, err := store.Resolve(ctx, resourceDigest.String())
		r.NoError(err)

		referrers, err := orasregistry.Referrers(ctx, graphStore, subject, oci.OwnershipArtifactType)
		r.NoError(err)
		r.Len(referrers, 1, "exactly one ownership referrer should be discoverable via the Referrers API")

		ref := referrers[0]

		t.Run("software.ocm.component.name and .version", func(t *testing.T) {
			assert.Equal(t, componentName, ref.Annotations[annotations.OwnershipComponentName])
			assert.Equal(t, componentVersion, ref.Annotations[annotations.OwnershipComponentVersion])
		})

		t.Run("software.ocm.artifact (identity and kind)", func(t *testing.T) {
			var payload struct {
				Identity map[string]string `json:"identity"`
				Kind     string            `json:"kind"`
			}
			require.NoError(t, json.Unmarshal([]byte(ref.Annotations[annotations.ArtifactAnnotationKey]), &payload))
			assert.Equal(t, "resource", payload.Kind)
			assert.Equal(t, resourceName, payload.Identity["name"])
			assert.Equal(t, componentVersion, payload.Identity["version"])
		})
	})
}
