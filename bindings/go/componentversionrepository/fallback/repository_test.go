package fallback

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resolverruntime "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/runtime"
	ociprovider "ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	transportArchive            = "./testdata/transport-archive"
	fallbackTransportArchive    = "./testdata/fallback-transport-archive"
	nonExistingTransportArchive = "./testdata/non-existing-fallback-transport-archive"
	transportArchiveCopy        = "./testdata/transport-archive-copy"
	componentName               = "github.com/acme.org/helloworld"
	componentVersion            = "1.0.0"
)

func Test_GetComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	registry := componentversionrepository.NewComponentVersionRepositoryRegistry(ctx)
	r.NoError(componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(repository.Scheme, registry, ociprovider.NewComponentVersionRepositoryProvider(), &ctf.Repository{}))

	transportArchiveRepoSpec := &ctfspec.Repository{
		Path:       transportArchive,
		AccessMode: ctfspec.AccessModeReadWrite,
	}

	transportArchiveCopyRepoSpec := &ctfspec.Repository{
		Path:       transportArchiveCopy,
		AccessMode: ctfspec.AccessModeReadWrite,
	}

	fallbackTransportArchiveRepoSpec := &ctfspec.Repository{
		Path:       fallbackTransportArchive,
		AccessMode: ctfspec.AccessModeReadOnly,
	}

	cases := []struct {
		name         string
		component    string
		version      string
		resolvers    []*resolverruntime.Resolver
		expectedRepo runtime.Typed
		err          assert.ErrorAssertionFunc
	}{
		{
			name:      "found without fallback",
			component: componentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: transportArchiveRepoSpec,
			err:          assert.NoError,
		},
		{
			name:      "found with fallback",
			component: componentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: fallbackTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: transportArchiveRepoSpec,
			err:          assert.NoError,
		},
		{
			name:      "higher priority first",
			component: componentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveCopyRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   20,
				},
			},
			expectedRepo: transportArchiveRepoSpec,
			err:          assert.NoError,
		},
		{
			name:      "same priority, first in list first",
			component: componentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveCopyRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: transportArchiveCopyRepoSpec,
			err:          assert.NoError,
		},
		{
			name:      "prefix matched",
			component: componentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveCopyRepoSpec,
					Prefix:     "github.com/not-acme.org",
					Priority:   0,
				},
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "github.com/acme.org",
					Priority:   0,
				},
			},
			expectedRepo: transportArchiveRepoSpec,
			err:          assert.NoError,
		},
		{
			name:      "partial prefix matched",
			component: componentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "github.com/ac",
					Priority:   0,
				},
			},
			expectedRepo: transportArchiveRepoSpec,
			err:          assert.NoError,
		},
		{
			name:      "not found with fallback",
			component: "github.com/not-acme.org/non-existing-component",
			version:   "1.0.0",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: transportArchiveCopyRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: fallbackTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: nil,
			err:          assert.Error,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			fallbackRepo, err := New(t.Context(), tc.resolvers, registry, nil)
			r.NoError(err)

			desc, err := fallbackRepo.GetComponentVersion(ctx, tc.component, tc.version)
			if !tc.err(t, err) {
				return
			}
			if tc.expectedRepo == nil {
				return
			}
			if desc != nil {
				r.Equal(tc.component, desc.Component.Name)
				r.Equal(tc.version, desc.Component.Version)
			}
			repo := fallbackRepo.repositoryForComponentCache[tc.component].resolver.Repository
			assert.True(t, reflect.DeepEqual(repo, tc.expectedRepo))
		})
	}
}
