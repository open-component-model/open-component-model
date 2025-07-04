package fallback

import (
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	resolverruntime "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/runtime"
	ctf "ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ociprovider "ocm.software/open-component-model/bindings/go/oci/repository/provider"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	transportArchive            = "./testdata/transport-archive"
	fallbackTransportArchive    = "./testdata/fallback-transport-archive"
	nonExistingTransportArchive = "./testdata/non-existing-fallback-transport-archive"
	transportArchiveCopy        = "./testdata/transport-archive-copy"
	helloWorldComponentName     = "github.com/acme.org/helloworld"
	notHelloWorldComponentName  = "github.com/acme.org/not-helloworld"
	componentVersion            = "1.0.0"
)

func Test_GetComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	registry := componentversionrepository.NewComponentVersionRepositoryRegistry(ctx)
	r.NoError(componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(repository.Scheme, registry, ociprovider.NewComponentVersionRepositoryProvider(), &ctfv1.Repository{}))

	transportArchiveRepoSpec := &ctfv1.Repository{
		Path:       transportArchive,
		AccessMode: ctfv1.AccessModeReadWrite,
	}

	transportArchiveCopyRepoSpec := &ctfv1.Repository{
		Path:       transportArchiveCopy,
		AccessMode: ctfv1.AccessModeReadWrite,
	}

	fallbackTransportArchiveRepoSpec := &ctfv1.Repository{
		Path:       fallbackTransportArchive,
		AccessMode: ctfv1.AccessModeReadWrite,
	}

	nonExistingTransportArchiveRepoSpec := &ctfv1.Repository{
		Path:       nonExistingTransportArchive,
		AccessMode: ctfv1.AccessModeReadWrite,
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
			component: helloWorldComponentName,
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
			component: helloWorldComponentName,
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
			component: helloWorldComponentName,
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
			component: helloWorldComponentName,
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
			component: helloWorldComponentName,
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
			component: helloWorldComponentName,
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
			version:   componentVersion,
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
		{
			name:      "fail with non-existing fallback",
			component: helloWorldComponentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: nonExistingTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: nil,
			err:          assert.Error,
		},
		{
			name:      "succeed if non-existing fallback is not used",
			component: helloWorldComponentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: nonExistingTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: nil,
			err:          assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+" (get)", func(t *testing.T) {
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

func Test_ListComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	registry := componentversionrepository.NewComponentVersionRepositoryRegistry(ctx)
	r.NoError(componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(repository.Scheme, registry, ociprovider.NewComponentVersionRepositoryProvider(), &ctfv1.Repository{}))

	transportArchiveRepoSpec := &ctfv1.Repository{
		Path:       transportArchive,
		AccessMode: ctfv1.AccessModeReadWrite,
	}

	transportArchiveCopyRepoSpec := &ctfv1.Repository{
		Path:       transportArchiveCopy,
		AccessMode: ctfv1.AccessModeReadWrite,
	}

	fallbackTransportArchiveRepoSpec := &ctfv1.Repository{
		Path:       fallbackTransportArchive,
		AccessMode: ctfv1.AccessModeReadOnly,
	}

	cases := []struct {
		name             string
		component        string
		resolvers        []*resolverruntime.Resolver
		expectedVersions []string
		err              assert.ErrorAssertionFunc
	}{
		{
			name:      "list versions from single repository",
			component: helloWorldComponentName,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedVersions: []string{"1.0.0"},
			err:              assert.NoError,
		},
		{
			name:      "deduplicate versions found in multiple repositories",
			component: helloWorldComponentName,
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
			},
			expectedVersions: []string{"1.0.0"},
			err:              assert.NoError,
		},
		{
			name:      "list versions accumulated from multiple repositories",
			component: helloWorldComponentName,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: fallbackTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedVersions: []string{"1.0.0", "2.0.0"},
			err:              assert.NoError,
		},
		{
			name:      "list versions accumulated from multiple repositories with priorities",
			component: helloWorldComponentName,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: fallbackTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedVersions: []string{"1.0.0", "2.0.0"},
			err:              assert.NoError,
		},
		{
			name:      "ignore repositories with wrong prefix",
			component: helloWorldComponentName,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "github.com/not-acme.org",
					Priority:   20,
				},
				{
					Repository: fallbackTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedVersions: []string{"2.0.0"},
			err:              assert.NoError,
		},
		{
			name:      "fail entirely if one repository fails",
			component: helloWorldComponentName,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: fallbackTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedVersions: []string{"2.0.0"},
			err:              assert.NoError,
		},
		{
			name:      "list versions from fallback repository only",
			component: notHelloWorldComponentName,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: transportArchiveRepoSpec,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: fallbackTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedVersions: []string{"1.0.0"},
			err:              assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			fallbackRepo, err := New(t.Context(), tc.resolvers, registry, nil)
			r.NoError(err)

			versions, err := fallbackRepo.ListComponentVersions(ctx, tc.component)
			if !tc.err(t, err) {
				return
			}
			assert.Equal(t, tc.expectedVersions, versions, "Expected versions for component %s", tc.component)
		})
	}
}

func Test_AddComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	registry := componentversionrepository.NewComponentVersionRepositoryRegistry(ctx)
	r.NoError(componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(repository.Scheme, registry, ociprovider.NewComponentVersionRepositoryProvider(), &ctfv1.Repository{}))

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)

	fallbackRepo, err := New(t.Context(), tc.resolvers, registry, nil)

	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo := Repository(t, ocictf.WithCTF(store))

	// Create a test component descriptor
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-component",
					Version: "1.0.0",
				},
			},
		},
	}
	_, err = repo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	r.Error(err)
	r.ErrorIs(err, oci.ErrNotFound)

	// Test adding component version
	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	err = repo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	desc2, err := repo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	r.NoError(err, "Failed to get component version after adding it")

	r.NotNil(desc2, "Component version should not be nil after adding it")
	r.Equal(desc.Component.Name, desc2.Component.Name, "Component name should match")
}
