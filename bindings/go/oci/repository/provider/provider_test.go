package provider_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfrepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// addNonDigestedResourceDescriptor uploads a local blob (so the AddComponentVersion local-blob
// existence check passes) and returns a component descriptor whose single resource references that
// blob but carries NO digest. This is the truthful discriminator between the two layouts: the
// normalized add path requires every resource to be digested and rejects this descriptor, while the
// default (v2) add path accepts it. See normalizedlayout.RequireAllResourcesDigested, which is only
// invoked on the normalized add path.
func addNonDigestedResourceDescriptor(t *testing.T, ctx context.Context, repo repository.ComponentVersionRepository) *descriptor.Descriptor {
	t.Helper()
	content := []byte("discriminator blob content")
	resource := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "test-resource", Version: "1.0.0"},
		},
		Type: "ociImageLayer",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(content).String(),
			MediaType:      ociImageSpecV1.MediaTypeImageLayer,
		},
	}
	newRes, err := repo.AddLocalResource(ctx, "example.org/comp", "1.0.0", resource, inmemory.New(bytes.NewReader(content)))
	require.NoError(t, err)

	// Drop the digest that AddLocalResource populated so the resource is undigested at add time.
	newRes.Digest = nil

	desc := &descriptor.Descriptor{}
	desc.Component.Name = "example.org/comp"
	desc.Component.Version = "1.0.0"
	desc.Component.Provider = descriptor.Provider{Name: "x"}
	desc.Component.Resources = append(desc.Component.Resources, *newRes)
	return desc
}

// TestProvider_ComponentVersionLayout_Normalized verifies end-to-end that a repository
// spec requesting the "normalized" layout is honored by the provider: the constructed
// repository stores component versions using the cosign-signable normalized layout.
//
// The provider does not expose the underlying store, so we prove the layout is active via two
// truthful, store-free signals:
//  1. A local resource round-trips through GetLocalResource on the normalized layout.
//  2. Adding a component whose resource lacks a digest is rejected with the normalized-layout
//     digest requirement — a check performed only on the normalized add path. The default layout
//     accepts the same descriptor (asserted by the negative control below).
func TestProvider_ComponentVersionLayout_Normalized(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)

	prov := provider.NewComponentVersionRepositoryProvider()

	repoSpec := &ctfrepospecv1.Repository{
		FilePath:               fs.String(),
		AccessMode:             ctfrepospecv1.AccessModeReadWrite,
		ComponentVersionLayout: "normalized",
	}

	repo, err := prov.GetComponentVersionRepository(ctx, repoSpec, nil)
	require.NoError(t, err)

	// Signal 2: the normalized add path enforces the resource-digest requirement.
	require.ErrorContains(t, repo.AddComponentVersion(ctx, addNonDigestedResourceDescriptor(t, ctx, repo)),
		"normalized layout requires every resource to be digested",
		"expected the normalized-layout digest requirement, proving the layout was honored")

	// Signal 1: a properly digested local resource round-trips via the normalized read path.
	desc := &descriptor.Descriptor{}
	desc.Component.Name = "example.org/comp"
	desc.Component.Version = "1.0.0"
	desc.Component.Provider = descriptor.Provider{Name: "x"}

	content := []byte("normalized provider local resource")
	resource := &descriptor.Resource{
		Relation: descriptor.LocalRelation,
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "test-resource", Version: "1.0.0"},
		},
		Type: "ociImageLayer",
		Access: &v2.LocalBlob{
			LocalReference: digest.FromBytes(content).String(),
			MediaType:      ociImageSpecV1.MediaTypeImageLayer,
		},
	}
	newRes, err := repo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, resource, inmemory.New(bytes.NewReader(content)))
	require.NoError(t, err)
	desc.Component.Resources = append(desc.Component.Resources, *newRes)

	require.NoError(t, repo.AddComponentVersion(ctx, desc))

	// Round-trips through the normalized read path.
	got, err := repo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	require.NoError(t, err)
	require.Equal(t, "example.org/comp", got.Component.Name)
	require.Equal(t, "1.0.0", got.Component.Version)

	blb, gotRes, err := repo.GetLocalResource(ctx, desc.Component.Name, desc.Component.Version, runtime.Identity{"name": "test-resource"})
	require.NoError(t, err)
	require.NotNil(t, gotRes)
	require.Equal(t, "test-resource", gotRes.Name)
	reader, err := blb.ReadCloser()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	roundTripped, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, content, roundTripped)
}

// TestProvider_ComponentVersionLayout_DefaultIsNotNormalized is the negative control for
// TestProvider_ComponentVersionLayout_Normalized: a spec without ComponentVersionLayout must use
// the default layout, which does NOT enforce the normalized resource-digest requirement. Adding a
// non-digested resource therefore succeeds, proving the default layout is not the normalized one.
func TestProvider_ComponentVersionLayout_DefaultIsNotNormalized(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)

	prov := provider.NewComponentVersionRepositoryProvider()

	repoSpec := &ctfrepospecv1.Repository{
		FilePath:   fs.String(),
		AccessMode: ctfrepospecv1.AccessModeReadWrite,
	}

	repo, err := prov.GetComponentVersionRepository(ctx, repoSpec, nil)
	require.NoError(t, err)

	// The default layout does not enforce the normalized digest requirement, so the same
	// descriptor that the normalized layout rejects is accepted here.
	require.NoError(t, repo.AddComponentVersion(ctx, addNonDigestedResourceDescriptor(t, ctx, repo)))
}

func Test_Provider_Smoke(t *testing.T) {
	t.Parallel()
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)

	prov := provider.NewComponentVersionRepositoryProvider()

	r := require.New(t)
	repoSpec := &ctfrepospecv1.Repository{FilePath: fs.String(), AccessMode: ctfrepospecv1.AccessModeReadWrite}
	_, err = prov.GetComponentVersionRepositoryCredentialConsumerIdentity(t.Context(), repoSpec)
	r.Error(err)

	t.Run("access provider concurrently", func(t *testing.T) {
		r := require.New(t)

		desc := descriptor.Descriptor{}
		desc.Meta.Version = "v2"
		desc.Component.Name = "github.com/ocm/test-component"
		desc.Component.Labels = append(desc.Component.Labels, descriptor.Label{Name: "foo", Value: []byte(`"bar"`)})
		desc.Component.Provider.Name = "ocm.software/open-component-model/bindings/go/oci/integration/test"

		t.Run("different versions", func(t *testing.T) {
			t.Parallel()

			retrievedDescs := make([]*descriptor.Descriptor, 10)
			retrievedVersions := make([][]string, 10)
			eg, ctx := errgroup.WithContext(t.Context())

			for i := 0; i < 10; i++ {
				eg.Go(func() error {
					d := desc
					d.Component.Version = fmt.Sprintf("v1.0.%d", i)
					repo, err := prov.GetComponentVersionRepository(ctx, repoSpec, nil)
					if err != nil {
						return fmt.Errorf("failed to get component version repository: %v", err)
					}
					err = repo.AddComponentVersion(ctx, &d)
					if err != nil {
						return fmt.Errorf("failed to add component version: %v", err)
					}
					retrievedDescs[i], err = repo.GetComponentVersion(ctx, d.Component.Name, d.Component.Version)
					if err != nil {
						return fmt.Errorf("failed to get component version: %v", err)
					}
					retrievedVersions[i], err = repo.ListComponentVersions(ctx, d.Component.Name)
					if err != nil {
						return fmt.Errorf("failed to list component versions for index %d: %v", i, err)
					}
					return nil
				})
			}
			r.NoError(eg.Wait())

			for i := 0; i < 10; i++ {
				r.Equal(desc.Component.Name, retrievedDescs[i].Component.Name)
				r.Equal(fmt.Sprintf("v1.0.%d", i), retrievedDescs[i].Component.Version)
				r.ElementsMatch(retrievedDescs[i].Component.Labels, desc.Component.Labels)
				r.Contains(retrievedVersions[i], fmt.Sprintf("v1.0.%d", i))
			}
		})
		t.Run("same version", func(t *testing.T) {
			t.Parallel()

			retrievedDescs := make([]*descriptor.Descriptor, 10)
			retrievedVersions := make([][]string, 10)
			eg, ctx := errgroup.WithContext(t.Context())
			d := desc
			d.Component.Version = "v1.0.0"
			for i := 0; i < 10; i++ {
				eg.Go(func() error {
					repo, err := prov.GetComponentVersionRepository(ctx, repoSpec, nil)
					if err != nil {
						return fmt.Errorf("failed to get component version repository: %v", err)
					}
					err = repo.AddComponentVersion(ctx, &d)
					if err != nil {
						return fmt.Errorf("failed to add component version: %v", err)
					}
					retrievedDescs[i], err = repo.GetComponentVersion(ctx, d.Component.Name, d.Component.Version)
					if err != nil {
						return fmt.Errorf("failed to get component version: %v", err)
					}
					retrievedVersions[i], err = repo.ListComponentVersions(ctx, d.Component.Name)
					if err != nil {
						return fmt.Errorf("failed to list component versions for index %d: %v", i, err)
					}
					return nil
				})
			}
			r.NoError(eg.Wait())

			for i := 0; i < 10; i++ {
				r.Equal(d.Component.Name, retrievedDescs[i].Component.Name)
				r.Equal(d.Component.Version, retrievedDescs[i].Component.Version)
				r.ElementsMatch(retrievedDescs[i].Component.Labels, d.Component.Labels)
				r.Contains(retrievedVersions[i], d.Component.Version)
			}
		})
	})

}

func Test_JSON_Schema_For_Repository_Specification(t *testing.T) {
	r := require.New(t)
	prov := provider.NewComponentVersionRepositoryProvider()

	cases := []struct {
		name               string
		inputType          runtime.Type
		expectErr          require.ErrorAssertionFunc
		expectedJSONSchema []byte
	}{
		{
			name:               "OCIRepository/v1 primary type",
			inputType:          runtime.NewVersionedType(ocirepospecv1.Type, "v1"),
			expectedJSONSchema: ocirepospecv1.Repository{}.JSONSchema(),
		},
		{
			name:               "CTF/v1 primary type",
			inputType:          runtime.NewVersionedType(ctfrepospecv1.Type, "v1"),
			expectedJSONSchema: ctfrepospecv1.Repository{}.JSONSchema(),
		},
		{
			name:      "Unknown type returns error",
			inputType: runtime.NewVersionedType("UnknownRepo", "v1"),
			expectErr: require.Error,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema, err := prov.GetJSONSchemaForRepositorySpecification(tc.inputType)
			if err != nil {
				tc.expectErr(t, err)
				return
			}
			r.NotEmpty(t, schema, "schema should not be empty for type %s", tc.inputType.String())
			r.Equal(tc.expectedJSONSchema, schema, "schema does not match expected for type %s", tc.inputType.String())
		})
	}
}

// TestWithHTTPConfig_CustomConfigIsUsed verifies that a custom HTTP config is
// used by the OCI provider for registry traffic by confirming the test server
// is actually contacted when a repository operation is performed.
func TestWithHTTPConfig_CustomConfigIsUsed(t *testing.T) {
	var serverHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHit = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	timeout := httpv1alpha1.NewTimeout(5 * time.Second)
	cfg := &httpv1alpha1.Config{TimeoutConfig: httpv1alpha1.TimeoutConfig{Timeout: timeout}}
	prov := provider.NewComponentVersionRepositoryProvider(
		provider.WithHTTPConfig(cfg),
	)
	require.NotNil(t, prov)

	repoSpec := &ocirepospecv1.Repository{
		BaseUrl: srv.URL,
		SubPath: "test/repo",
	}
	repo, err := prov.GetComponentVersionRepository(t.Context(), repoSpec, nil)
	require.NoError(t, err)
	// ListComponentVersions triggers HTTP traffic to the registry.
	_, _ = repo.ListComponentVersions(t.Context(), "example.org/component")
	require.True(t, serverHit, "expected HTTP request to reach test server")
}

// TestWithHTTPConfig_NilFallsBackToDefault verifies that when no HTTPConfig
// option is supplied the provider uses ocmhttp defaults (built on top of
// oras-go's retry transport) and can serve CTF-based repositories without panic.
func TestWithHTTPConfig_NilFallsBackToDefault(t *testing.T) {
	prov := provider.NewComponentVersionRepositoryProvider()
	require.NotNil(t, prov)

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)

	repoSpec := &ctfrepospecv1.Repository{
		FilePath:   fs.String(),
		AccessMode: ctfrepospecv1.AccessModeReadWrite,
	}
	repo, err := prov.GetComponentVersionRepository(t.Context(), repoSpec, nil)
	require.NoError(t, err)
	require.NotNil(t, repo)
}

// TestWithHTTPConfig_ShortTimeoutCausesError starts a server that hangs and
// verifies that a provider configured with a very short overall timeout
// returns an error when performing an OCI registry operation.
func TestWithHTTPConfig_ShortTimeoutCausesError(t *testing.T) {
	// Server that sleeps longer than our timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	timeout := httpv1alpha1.NewTimeout(10 * time.Millisecond)
	cfg := &httpv1alpha1.Config{TimeoutConfig: httpv1alpha1.TimeoutConfig{Timeout: timeout}}
	prov := provider.NewComponentVersionRepositoryProvider(
		provider.WithHTTPConfig(cfg),
	)

	repoSpec := &ocirepospecv1.Repository{
		BaseUrl: srv.URL,
		SubPath: "test/repo",
	}
	repo, err := prov.GetComponentVersionRepository(t.Context(), repoSpec, nil)
	require.NoError(t, err)
	// ListComponentVersions triggers HTTP traffic — should time out.
	_, err = repo.ListComponentVersions(t.Context(), "example.org/component")
	require.Error(t, err)
}
