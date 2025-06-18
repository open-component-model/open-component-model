package provider_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfrepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

func Test_Provider_Smoke(t *testing.T) {
	t.Parallel()
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)

	prov := provider.NewComponentVersionRepositoryProvider()

	r := require.New(t)
	repoSpec := &ctfrepospecv1.Repository{Path: fs.String(), AccessMode: ctfrepospecv1.AccessModeReadWrite}
	id, err := prov.GetComponentVersionRepositoryCredentialConsumerIdentity(t.Context(), repoSpec)
	r.NoError(err)

	r.Equal(id[ocmruntime.IdentityAttributePath], fs.String())

	repo1, err := prov.GetComponentVersionRepository(t.Context(), repoSpec, nil)
	r.NoError(err)
	r.NotNil(repo1)

	repo2, err := prov.GetComponentVersionRepository(t.Context(), repoSpec, nil)
	r.NoError(err)
	r.NotNil(repo2)

	t.Run("basic upload and download of a component version", func(t *testing.T) {
		name, version := "test-component", "v1.0.0"

		ctx := t.Context()
		r := require.New(t)

		desc := descriptor.Descriptor{}
		desc.Component.Name = name
		desc.Component.Version = version
		desc.Component.Labels = append(desc.Component.Labels, descriptor.Label{Name: "foo", Value: []byte(`"bar"`)})
		desc.Component.Provider.Name = "ocm.software/open-component-model/bindings/go/oci/integration/test"

		r.NoError(repo1.AddComponentVersion(ctx, &desc))

		// Verify that the component version can be retrieved
		retrievedDesc, err := repo1.GetComponentVersion(ctx, name, version)
		r.NoError(err)

		r.Equal(name, retrievedDesc.Component.Name)
		r.Equal(version, retrievedDesc.Component.Version)
		r.ElementsMatch(retrievedDesc.Component.Labels, desc.Component.Labels)

		versions, err := repo1.ListComponentVersions(ctx, name)
		r.NoError(err)
		r.Contains(versions, version)

		// Now verify that the same component version can be retrieved from the second repository
		retrievedDesc2, err := repo2.GetComponentVersion(ctx, name, version)
		r.NoError(err)
		r.Equal(name, retrievedDesc2.Component.Name)

		t.Run("concurrent access to the same repository", func(t *testing.T) {
			r := require.New(t)
			// Attempt to add the same component version concurrently
			eg, ctx := errgroup.WithContext(t.Context())
			for i := 0; i < 10; i++ {
				eg.Go(func() error {
					return repo1.AddComponentVersion(ctx, &desc)
				})
				eg.Go(func() error {
					return repo2.AddComponentVersion(ctx, &desc)
				})
			}
			r.NoError(eg.Wait())

			// Verify that the component version still exists and was not corrupted during write
			_, err := repo1.GetComponentVersion(ctx, name, version)
			r.NoError(err)
		})
	})
}
