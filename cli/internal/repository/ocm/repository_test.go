package ocm_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	resolverv1 "ocm.software/open-component-model/bindings/go/componentversionrepository/reference/resolver/config/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/plugin/builtin"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	testComponentReference              = testRepositoryPath + "//" + testComponentName + ":" + testComponentVersion
	testComponentReferenceWithoutPrefix = testComponentName + ":" + testComponentVersion
	testComponentName                   = "github.com/acme.org/helloworld"
	testComponentVersion                = "1.0.0"
	testRepositoryPath                  = "./testdata/transport-archive"
)

func TestRepository(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	t.Run("resolve without prefix", func(t *testing.T) {
		var rawRepo runtime.Raw
		r.NoError(repository.Scheme.Convert(&ctf.Repository{
			Path:       testRepositoryPath,
			AccessMode: ctf.AccessModeReadWrite,
		}, &rawRepo))
		pluginManager := manager.NewPluginManager(ctx)
		r.NoError(builtin.Register(pluginManager))
		_ = &resolverv1.Config{
			Type:    runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
			Aliases: nil,
			Resolvers: []*resolverv1.Resolver{
				{
					Repository: &rawRepo,
					Prefix:     "",
					Priority:   0,
				},
			},
		}
		//ref, err := compref.Parse(testComponentReferenceWithoutPrefix, &compref.ParseOptions{
		//	Aliases:           resolverConfig.Aliases,
		//	FallbackResolvers: resolverConfig.Resolvers,
		//})
		//r.NoError(err, "Failed to parse component reference")
		repo, err := ocm.New(ctx, pluginManager, nil, testComponentReferenceWithoutPrefix)
		r.NoError(err, "Failed to create component repository")
		desc, err := repo.GetComponentVersions(ctx, ocm.GetComponentVersionsOptions{
			VersionOptions: ocm.VersionOptions{
				LatestOnly: true,
			},
			ConcurrencyLimit: 1,
		})
		r.NoError(err, "Failed to get component versions")
		r.NotNil(desc, "Component version description should not be nil")
	})

	t.Run("resolve with fallbacks", func(t *testing.T) {
		var rawRepo1 runtime.Raw
		r.NoError(repository.Scheme.Convert(&ctf.Repository{
			Path:       testRepositoryPath,
			AccessMode: ctf.AccessModeReadWrite,
		}, &rawRepo1))

		var rawRepo2 runtime.Raw
		r.NoError(repository.Scheme.Convert(&ctf.Repository{
			Path:       "./testdata/transport-archive-without-component",
			AccessMode: ctf.AccessModeReadWrite,
		}, &rawRepo2))

		pluginManager := manager.NewPluginManager(ctx)
		r.NoError(builtin.Register(pluginManager))
		_ = &resolverv1.Config{
			Type:    runtime.NewVersionedType(resolverv1.ConfigType, resolverv1.Version),
			Aliases: nil,
			Resolvers: []*resolverv1.Resolver{
				{
					Repository: &rawRepo2,
					Prefix:     "",
					Priority:   10,
				},
				{
					Repository: &rawRepo1,
					Prefix:     "",
					Priority:   0,
				},
			},
		}
		//ref, err := compref.Parse(testComponentReferenceWithoutPrefix, &compref.ParseOptions{
		//	Aliases:           resolverConfig.Aliases,
		//	FallbackResolvers: resolverConfig.Resolvers,
		//})
		//r.NoError(err, "Failed to parse component reference")
		repo, err := ocm.New(ctx, pluginManager, nil, testComponentReferenceWithoutPrefix)
		r.NoError(err, "Failed to create component repository")
		desc, err := repo.GetComponentVersions(ctx, ocm.GetComponentVersionsOptions{
			VersionOptions: ocm.VersionOptions{
				LatestOnly: true,
			},
			ConcurrencyLimit: 1,
		})
		r.NoError(err, "Failed to get component versions")
		r.NotEmpty(desc, "Component version description should not be nil")
		desc, err = repo.GetComponentVersions(ctx, ocm.GetComponentVersionsOptions{
			VersionOptions: ocm.VersionOptions{
				LatestOnly: true,
			},
			ConcurrencyLimit: 1,
		})
		r.NoError(err, "Failed to get component versions")
		r.NotEmpty(desc, "Component version description should not be nil")
	})
}
