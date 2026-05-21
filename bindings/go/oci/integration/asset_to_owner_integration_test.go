package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/log"
	"github.com/testcontainers/testcontainers-go/modules/registry"
	"oras.land/oras-go/v2/registry/remote"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/resource"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/spec/ownership"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// Test_Integration_AssetToOwner verifies the asset-to-owner scenario
// end-to-end (ADR 0016): a by-value OCI resource uploaded through the OCM
// OCI binding must be discoverable as an ownership referrer via the OCI
// Distribution Referrers API.
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

	authClient := createAuthClient(registryAddress, testUsername, password)
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(registryAddress),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(authClient),
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

	t.Run("create component version and verify single ownership referrer", func(t *testing.T) {
		r := require.New(t)
		resourceDigest := uploadResource(t, ctx, repo, componentName, componentVersion, resourceName, []byte("ownership-payload"))

		owners := lookupOwners(t, ctx, resolver, authClient, componentName, componentVersion, resourceDigest)
		r.Len(owners, 1, "exactly one ownership referrer should be discoverable via the Referrers API")
		owner := owners[0]

		t.Run("software.ocm.component.name and .version", func(t *testing.T) {
			assert.Equal(t, componentName, owner.ComponentName)
			assert.Equal(t, componentVersion, owner.ComponentVersion)
		})

		t.Run("software.ocm.artifact (identity and kind)", func(t *testing.T) {
			assert.Equal(t, annotations.ArtifactKindResource, owner.Artifact.Kind)
			assert.Equal(t, resourceName, owner.Artifact.Identity[descriptor.IdentityAttributeName])
			assert.Equal(t, componentVersion, owner.Artifact.Identity[descriptor.IdentityAttributeVersion])
		})
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
				owners := lookupOwners(t, ctx, resolver, authClient, multiComponent, componentVersion, tc.subject)
				require.Len(t, owners, 1, "exactly one referrer per asset")
				assert.Equal(t, tc.want, owners[0].Artifact.Identity[descriptor.IdentityAttributeName],
					"%s referrer must point at its own asset, not the sibling", tc.label)
			})
		}
	})

	t.Run("re-uploading the same resource leaves a single referrer", func(t *testing.T) {
		// The referrer manifest omits org.opencontainers.image.created, so every
		// re-upload produces an identical manifest digest and the registry returns
		// the existing one instead of indexing a new referrer. End-to-end proof
		// of `ocm add cv` idempotency at the referrer layer.
		var resourceDigest digest.Digest
		for i := range 3 {
			resourceDigest = uploadResource(t, ctx, repo, componentName, componentVersion, resourceName, []byte("ownership-payload"))
			require.NotEmptyf(t, resourceDigest, "re-upload attempt %d must yield a digest", i+1)
		}

		owners := lookupOwners(t, ctx, resolver, authClient, componentName, componentVersion, resourceDigest)
		assert.Lenf(t, owners, 1,
			"identical re-uploads must converge on a single referrer; got %d distinct manifests", len(owners))
	})

	t.Run("policy disabled: resource uploads without an ownership referrer", func(t *testing.T) {
		// Locks in the opt-out contract: a repository constructed without
		// [oci.WithOwnershipReferrerPolicy] (the default
		// [oci.OwnershipReferrerPolicyDisabled]) must accept a by-value
		// resource and leave the Referrers API empty for that subject.
		r := require.New(t)
		disabledRepo, err := oci.NewRepository(
			oci.WithResolver(resolver),
			oci.WithTempDir(t.TempDir()),
		)
		r.NoError(err)

		const (
			disabledComponent = "ocm.software/asset-to-owner-test-disabled"
			disabledResource  = "backend-image-disabled"
		)
		resourceDigest := uploadResource(t, ctx, disabledRepo, disabledComponent, componentVersion, disabledResource, []byte("ownership-payload-disabled"))

		owners := lookupOwners(t, ctx, resolver, authClient, disabledComponent, componentVersion, resourceDigest)
		assert.Emptyf(t, owners,
			"policy disabled must not push any ownership referrer; found %d", len(owners))
	})
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

// lookupOwners resolves the ownership records for subjectDigest through the
// public resourcerepo.LookupOwners entry point. The `http://` scheme prefix
// forces plain-HTTP transport against the test container registry.
func lookupOwners(t *testing.T, ctx context.Context, resolver *urlresolver.CachingResolver, client remote.Client, component, version string, subjectDigest digest.Digest) []ownership.Ownership {
	t.Helper()
	r := require.New(t)
	imageRef := fmt.Sprintf("http://%s@%s", resolver.ComponentVersionReference(ctx, component, version), subjectDigest)
	owners, err := resource.LookupOwners(ctx, imageRef, client)
	r.NoError(err)
	return owners
}
