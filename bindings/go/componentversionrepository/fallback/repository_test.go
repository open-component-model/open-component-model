package fallback

import (
	"testing"

	"github.com/stretchr/testify/require"
	resolverruntime "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/runtime"
	ociprovider "ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
)

const (
	primaryCtfPath   = "./testdata/transport-archive"
	fallbackCtfPath1 = "./testdata/fallback-transport-archive"
	fallbackCtfPath2 = "./testdata/non-existing-fallback-transport-archive"
	componentName    = "github.com/acme.org/helloworld"
	componentVersion = "1.0.0"
)

func Test_FallbackRepository(t *testing.T) {
	// can you write a test for the FallbackComponentVersionRepository?
}

func Test_GetComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	registry := componentversionrepository.NewComponentVersionRepositoryRegistry(ctx)
	r.NoError(componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(repository.Scheme, registry, ociprovider.NewComponentVersionRepositoryProvider(), &ctf.Repository{}))

	primaryRepoSpec := &ctfspec.Repository{
		Path:       primaryCtfPath,
		AccessMode: ctfspec.AccessModeReadWrite,
	}

	fallbackRepoSpec1 := &ctfspec.Repository{
		Path:       fallbackCtfPath1,
		AccessMode: ctfspec.AccessModeReadOnly,
	}

	//fallbackRepoSpec2 := &ctfspec.Repository{
	//	Path:       fallbackCtfPath2,
	//	AccessMode: ctfspec.AccessModeReadOnly,
	//}

	t.Run("get component version", func(t *testing.T) {
		r := require.New(t)

		fallbackRepo, err := New(t.Context(), primaryRepoSpec, registry, nil, &FallbackComponentVersionRepositoryOptions{
			FallbackResolvers: []*resolverruntime.Resolver{
				{
					Repository: fallbackRepoSpec1,
					Prefix:     "",
					Priority:   0,
				},
			},
		})
		r.NoError(err)

		desc, err := fallbackRepo.GetComponentVersion(ctx, componentName, componentVersion)
		r.NoError(err)
		r.NotNil(desc)
	})

	//tests := []struct {
	//	name              string
	//	mainShouldErr     bool
	//	fallbackShouldErr bool
	//	expectFallback    bool
	//	expectErr         bool
	//}{
	//	{
	//		name:           "found in main",
	//		mainShouldErr:  false,
	//		expectFallback: false,
	//		expectErr:      false,
	//	},
	//	{
	//		name:              "not found in main, found in fallback",
	//		mainShouldErr:     true,
	//		fallbackShouldErr: false,
	//		expectFallback:    true,
	//		expectErr:         false,
	//	},
	//	{
	//		name:              "not found in main or fallback",
	//		mainShouldErr:     true,
	//		fallbackShouldErr: true,
	//		expectFallback:    true,
	//		expectErr:         true,
	//	},
	//}
	//
	//for _, tt := range tests {
	//	t.Run(tt.name, func(t *testing.T) {
	//		r := require.New(t)
	//
	//		fallbackRepo, err := New(t.Context(), primaryRepoSpec, registry, nil, &FallbackComponentVersionRepositoryOptions{
	//			FallbackResolvers: []*resolverv1.Resolver{
	//				{
	//					Repository: fallbackRepoSpec1,
	//					Prefix:     "",
	//					Priority:   0,
	//				},
	//			},
	//		})
	//		r.NoError(err)
	//
	//		repo := &FallbackComponentVersionRepository{
	//			RepositoryRegistry:      registry,
	//			repository: mainSpec,
	//			credentialProvider:                   credentialProvider,
	//			fallbackRepositorySpecifications: []FallbackRepositorySpecification{
	//				{RepositorySpecification: fallbackSpec, Prefix: ""},
	//			},
	//		}
	//		desc, err := repo.GetComponentVersion(ctx, "comp", "1.0.0")
	//		if tt.expectErr {
	//			if err == nil {
	//				t.Fatalf("expected error, got nil")
	//			}
	//		} else {
	//			if err != nil {
	//				t.Fatalf("unexpected error: %v", err)
	//			}
	//			if desc == nil {
	//				t.Fatalf("expected descriptor, got nil")
	//			}
	//			if desc.Name != "comp" || desc.Version != "1.0.0" {
	//				t.Fatalf("unexpected descriptor: %+v", desc)
	//			}
	//		}
	//	})
	//}
}
