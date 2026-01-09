package provider_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfrepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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
