package provider_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	ownershipv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/ownership/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	repoSpec "ocm.software/open-component-model/bindings/go/oci/spec/repository"
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
	ctfSpec := &ctfrepospecv1.Repository{FilePath: fs.String(), AccessMode: ctfrepospecv1.AccessModeReadWrite}
	_, err = prov.GetComponentVersionRepositoryCredentialConsumerIdentity(t.Context(), ctfSpec)
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
					repo, err := prov.GetComponentVersionRepository(ctx, ctfSpec, nil)
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
					repo, err := prov.GetComponentVersionRepository(ctx, ctfSpec, nil)
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

func TestResolveOwnershipReferrerPolicy(t *testing.T) {
	rawSpec := func(t *testing.T, jsonStr string) *runtime.Raw {
		t.Helper()
		raw := &runtime.Raw{}
		require.NoError(t, json.Unmarshal([]byte(jsonStr), raw))
		return raw
	}

	makeCfg := func(t *testing.T, policy ownershipv1alpha1.Policy, repos ...*ownershipv1alpha1.RepositoryPolicy) *ownershipv1alpha1.Config {
		t.Helper()
		return &ownershipv1alpha1.Config{Policy: policy, Repositories: repos}
	}

	targetOCIGhcrMyOrg := rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io","subPath":"my-org/components"}`)
	targetOCIGhcrEmbeddedPath := rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io/my-org/components"}`)
	targetUnknownType := rawSpec(t, `{"type":"UnregisteredRepository/v1"}`)

	tests := []struct {
		name    string
		cfg     *ownershipv1alpha1.Config
		target  *runtime.Raw
		want    oci.OwnershipReferrerPolicy
		wantErr bool
	}{
		{
			name:   "nil config → never",
			cfg:    nil,
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyNever,
		},
		{
			name:   "empty config → never",
			cfg:    &ownershipv1alpha1.Config{},
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyNever,
		},
		{
			name:   "top-level AddIfSupported, no overrides",
			cfg:    makeCfg(t, ownershipv1alpha1.PolicyAddIfSupported),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name:   "top-level Never, no overrides",
			cfg:    makeCfg(t, ownershipv1alpha1.PolicyNever),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyNever,
		},
		{
			name:    "unsupported top-level policy → error",
			cfg:     makeCfg(t, "Bogus"),
			target:  targetOCIGhcrMyOrg,
			wantErr: true,
		},
		{
			name: "type-only override matches any spec of same type",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "specific override beats top-level fallback",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io","subPath":"my-org/components"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "specific entry listed before wildcard wins",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever,
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io","subPath":"my-org/components"}`),
					Policy:     ownershipv1alpha1.PolicyAddIfSupported,
				},
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1"}`),
					Policy:     ownershipv1alpha1.PolicyNever,
				},
			),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "first matching entry wins among multiple matches",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever,
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
					Policy:     ownershipv1alpha1.PolicyAddIfSupported,
				},
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
					Policy:     ownershipv1alpha1.PolicyNever,
				},
			),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "non-matching override falls through to top-level",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyAddIfSupported, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"docker.io"}`),
				Policy:     ownershipv1alpha1.PolicyNever,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "different type never matches",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"S3/v1","baseUrl":"s3.aws"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyNever,
		},
		{
			name: "baseUrl with embedded path matches split baseUrl+subPath form",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io","subPath":"my-org/components"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrEmbeddedPath,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "empty top-level policy with matching entry applies entry policy",
			cfg: makeCfg(t, "", &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "empty top-level policy with no matching entry → never",
			cfg: makeCfg(t, "", &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"docker.io"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyNever,
		},
		{
			name: "nil entries in repositories list are skipped",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever,
				nil,
				&ownershipv1alpha1.RepositoryPolicy{Repository: nil, Policy: ownershipv1alpha1.PolicyAddIfSupported},
				&ownershipv1alpha1.RepositoryPolicy{
					Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
					Policy:     ownershipv1alpha1.PolicyAddIfSupported,
				},
			),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "unsupported entry policy → error after matching",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
				Policy:     "Bogus",
			}),
			target:  targetOCIGhcrMyOrg,
			wantErr: true,
		},
		{
			name:   "unknown target type with no entries skips identity extraction",
			cfg:    makeCfg(t, ownershipv1alpha1.PolicyAddIfSupported),
			target: targetUnknownType,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "unknown target type with entries fails identity extraction",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"UnregisteredRepository/v1"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target:  targetUnknownType,
			wantErr: true,
		},
		{
			name: "registered type alias on entry still matches canonical target type",
			cfg: makeCfg(t, ownershipv1alpha1.PolicyNever, &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCI","baseUrl":"ghcr.io"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target: targetOCIGhcrMyOrg,
			want:   oci.OwnershipReferrerPolicyAddIfSupported,
		},
		{
			name: "invalid top-level policy is rejected even when an entry matches",
			cfg: makeCfg(t, "Bogus", &ownershipv1alpha1.RepositoryPolicy{
				Repository: rawSpec(t, `{"type":"OCIRepository/v1","baseUrl":"ghcr.io"}`),
				Policy:     ownershipv1alpha1.PolicyAddIfSupported,
			}),
			target:  targetOCIGhcrMyOrg,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := provider.ResolveOwnershipReferrerPolicy(repoSpec.Scheme, tc.cfg, tc.target)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
