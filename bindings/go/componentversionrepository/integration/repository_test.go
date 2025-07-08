package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/componentversionrepository/fallback"
	resolverruntime "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
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
	resourceName                = "resource"
	resourceVersion             = "6.7.1"
	sourceName                  = "source"
	sourceVersion               = "6.7.1"
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
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			var logBuffer bytes.Buffer
			logHandler := slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})
			logger := slog.New(logHandler)
			slog.SetDefault(logger)

			provider := ociprovider.NewComponentVersionRepositoryProvider()

			fallbackRepo, err := fallback.New(t.Context(), tc.resolvers, provider, nil)
			r.NoError(err)

			desc, err := fallbackRepo.GetComponentVersion(ctx, tc.component, tc.version)
			if !tc.err(t, err) {
				return
			}
			if tc.expectedRepo == nil {
				return
			}
			r.NotNil(desc, "Expected descriptor to be not nil for component %s and version %s", tc.component, tc.version)
			r.Equal(tc.component, desc.Component.Name)
			r.Equal(tc.version, desc.Component.Version)

			expectedRepo, err := json.Marshal(tc.expectedRepo)
			r.NoError(err, "Failed to marshal expected repository")
			expectedLog := fmt.Sprintf(`"msg":"repository used for operation","realm":"%s","component":"%s","repository":%s`, fallback.Realm, tc.component, expectedRepo)
			r.Contains(logBuffer.String(), expectedLog)
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

	nonExistingTransportArchiveRepoSpec := &ctfv1.Repository{
		Path:       nonExistingTransportArchive,
		AccessMode: ctfv1.AccessModeReadWrite,
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
					Repository: nonExistingTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: fallbackTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedVersions: nil,
			err:              assert.Error,
		},
		{
			name:      "fail entirely if one repository fails independent of order",
			component: helloWorldComponentName,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: fallbackTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: nonExistingTransportArchiveRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedVersions: nil,
			err:              assert.Error,
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

			provider := ociprovider.NewComponentVersionRepositoryProvider()

			fallbackRepo, err := fallback.New(t.Context(), tc.resolvers, provider, nil)
			r.NoError(err)

			versions, err := fallbackRepo.ListComponentVersions(ctx, tc.component)
			if !tc.err(t, err) {
				return
			}
			r.Equal(tc.expectedVersions, versions, "Expected versions for component %s", tc.component)
		})
	}
}

func Test_AddComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	provider := ociprovider.NewComponentVersionRepositoryProvider()

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)

	repo := ctfv1.Repository{
		Path:       fs.String(),
		AccessMode: ctfv1.AccessModeReadWrite,
	}

	fallbackRepo, err := fallback.New(t.Context(), []*resolverruntime.Resolver{
		{
			Repository: &repo,
			Prefix:     "",
			Priority:   0,
		},
	}, provider, nil)
	r.NoError(err, "failed to create fallback repository")

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
	_, err = fallbackRepo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	r.Error(err)
	r.ErrorIs(err, oci.ErrNotFound)

	// Test adding component version
	err = fallbackRepo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	err = fallbackRepo.AddComponentVersion(ctx, desc)
	r.NoError(err, "Failed to add component version when it should succeed")

	desc2, err := fallbackRepo.GetComponentVersion(ctx, desc.Component.Name, desc.Component.Version)
	r.NoError(err, "Failed to get component version after adding it")

	r.NotNil(desc2, "Component version should not be nil after adding it")
	r.Equal(desc.Component.Name, desc2.Component.Name, "Component name should match")
}

func Test_GetLocalResource(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	registry := componentversionrepository.NewComponentVersionRepositoryRegistry(ctx)
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)
	r.NoError(componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(scheme, registry, ociprovider.NewComponentVersionRepositoryProvider(), &ctfv1.Repository{}))

	transportArchiveRepoSpec := &ctfv1.Repository{
		Path:       transportArchive,
		AccessMode: ctfv1.AccessModeReadWrite,
	}

	fallbackTransportArchiveRepoSpec := &ctfv1.Repository{
		Path:       fallbackTransportArchive,
		AccessMode: ctfv1.AccessModeReadWrite,
	}

	cases := []struct {
		name             string
		component        string
		version          string
		resourceIdentity runtime.Identity
		resolvers        []*resolverruntime.Resolver
		expectedRepo     runtime.Typed
		err              assert.ErrorAssertionFunc
	}{
		{
			name:      "found without fallback",
			component: helloWorldComponentName,
			version:   componentVersion,
			resourceIdentity: map[string]string{
				descriptor.IdentityAttributeName:    resourceName,
				descriptor.IdentityAttributeVersion: resourceVersion,
			},
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
			component: notHelloWorldComponentName,
			version:   componentVersion,
			resourceIdentity: map[string]string{
				descriptor.IdentityAttributeName:    resourceName,
				descriptor.IdentityAttributeVersion: resourceVersion,
			},
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
			expectedRepo: fallbackTransportArchiveRepoSpec,
			err:          assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			var logBuffer bytes.Buffer
			logHandler := slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})
			logger := slog.New(logHandler)
			slog.SetDefault(logger)

			provider := ociprovider.NewComponentVersionRepositoryProvider()
			fallbackRepo, err := fallback.New(t.Context(), tc.resolvers, provider, nil)
			r.NoError(err)

			blob, resource, err := fallbackRepo.GetLocalResource(ctx, tc.component, tc.version, tc.resourceIdentity)
			if !tc.err(t, err) {
				return
			}
			if tc.expectedRepo == nil {
				return
			}
			r.Equal(tc.resourceIdentity, resource.ToIdentity(), "Expected resource identity to match %s for component %s and version %s", tc.resourceIdentity, tc.component, tc.version)
			r.NotNil(blob, "Expected blob to be not nil for component %s and version %s", tc.component, tc.version)

			expectedRepo, err := json.Marshal(tc.expectedRepo)
			r.NoError(err, "Failed to marshal expected repository")
			expectedLog := fmt.Sprintf(`"msg":"repository used for operation","realm":"%s","component":"%s","repository":%s`, fallback.Realm, tc.component, expectedRepo)
			r.Contains(logBuffer.String(), expectedLog)
		})
	}
}

func Test_GetLocalSource(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	registry := componentversionrepository.NewComponentVersionRepositoryRegistry(ctx)
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	v2.MustAddToScheme(scheme)
	r.NoError(componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(scheme, registry, ociprovider.NewComponentVersionRepositoryProvider(), &ctfv1.Repository{}))

	transportArchiveRepoSpec := &ctfv1.Repository{
		Path:       transportArchive,
		AccessMode: ctfv1.AccessModeReadWrite,
	}

	fallbackTransportArchiveRepoSpec := &ctfv1.Repository{
		Path:       fallbackTransportArchive,
		AccessMode: ctfv1.AccessModeReadWrite,
	}

	cases := []struct {
		name             string
		component        string
		version          string
		resourceIdentity runtime.Identity
		resolvers        []*resolverruntime.Resolver
		expectedRepo     runtime.Typed
		err              assert.ErrorAssertionFunc
	}{
		{
			name:      "found without fallback",
			component: helloWorldComponentName,
			version:   componentVersion,
			resourceIdentity: map[string]string{
				descriptor.IdentityAttributeName:    sourceName,
				descriptor.IdentityAttributeVersion: sourceVersion,
			},
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
			component: notHelloWorldComponentName,
			version:   componentVersion,
			resourceIdentity: map[string]string{
				descriptor.IdentityAttributeName:    sourceName,
				descriptor.IdentityAttributeVersion: sourceVersion,
			},
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
			expectedRepo: fallbackTransportArchiveRepoSpec,
			err:          assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			var logBuffer bytes.Buffer
			logHandler := slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})
			logger := slog.New(logHandler)
			slog.SetDefault(logger)

			provider := ociprovider.NewComponentVersionRepositoryProvider()
			fallbackRepo, err := fallback.New(t.Context(), tc.resolvers, provider, nil)
			r.NoError(err)

			blob, source, err := fallbackRepo.GetLocalSource(ctx, tc.component, tc.version, tc.resourceIdentity)
			if !tc.err(t, err) {
				return
			}
			if tc.expectedRepo == nil {
				return
			}
			r.Equal(tc.resourceIdentity, source.ToIdentity(), "Expected resource identity to match %s for component %s and version %s", tc.resourceIdentity, tc.component, tc.version)
			r.NotNil(blob, "Expected blob to be not nil for component %s and version %s", tc.component, tc.version)

			expectedRepo, err := json.Marshal(tc.expectedRepo)
			r.NoError(err, "Failed to marshal expected repository")
			expectedLog := fmt.Sprintf(`"msg":"repository used for operation","realm":"%s","component":"%s","repository":%s`, fallback.Realm, tc.component, expectedRepo)
			r.Contains(logBuffer.String(), expectedLog)
		})
	}
}

func Test_AddLocalResource(t *testing.T) {
	ctx := t.Context()

	provider := ociprovider.NewComponentVersionRepositoryProvider()

	resourceContentString := "test layer content"
	resourceContent := inmemory.New(bytes.NewReader([]byte(resourceContentString)))
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    resourceName,
				Version: resourceVersion,
			},
		},
		Type: "my-type",
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			Data: []byte(fmt.Sprintf(
				`{"type":"%s","localReference":"%s","mediaType":"%s"}`,
				runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
				digest.FromString(resourceContentString).String(),
				"my-media-type",
			)),
		},
		Digest:       nil,
		CreationTime: descriptor.CreationTime{},
	}

	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    helloWorldComponentName,
					Version: componentVersion,
				},
			},
		},
	}

	t.Run("add local resource without fallback", func(t *testing.T) {
		r := require.New(t)

		fsWithComponent, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
		r.NoError(err)

		repoWithComponent := ctfv1.Repository{
			Path:       fsWithComponent.String(),
			AccessMode: ctfv1.AccessModeReadWrite,
		}

		fallbackRepo, err := fallback.New(t.Context(), []*resolverruntime.Resolver{
			{
				Repository: &repoWithComponent,
				Prefix:     "",
				Priority:   0,
			},
		}, provider, nil)
		r.NoError(err, "failed to create fallback repository")

		res, err := fallbackRepo.AddLocalResource(ctx, desc.Component.Name, desc.Component.Version, resource, resourceContent)

		r.NoError(err, "Failed to add local resource when it should succeed")
		r.NotNil(res, "Expected resource to be not nil after adding it")
	})
}

func Test_AddLocalSource(t *testing.T) {
	ctx := t.Context()

	provider := ociprovider.NewComponentVersionRepositoryProvider()

	sourceContentString := "test layer content"
	sourceContent := inmemory.New(bytes.NewReader([]byte(sourceContentString)))
	source := &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    resourceName,
				Version: resourceVersion,
			},
		},
		Type: "my-type",
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			Data: []byte(fmt.Sprintf(
				`{"type":"%s","localReference":"%s","mediaType":"%s"}`,
				runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
				digest.FromString(sourceContentString).String(),
				"my-media-type",
			)),
		},
	}

	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    helloWorldComponentName,
					Version: componentVersion,
				},
			},
		},
	}

	t.Run("add local source without fallback", func(t *testing.T) {
		r := require.New(t)

		fsWithComponent, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
		r.NoError(err)

		repoWithComponent := ctfv1.Repository{
			Path:       fsWithComponent.String(),
			AccessMode: ctfv1.AccessModeReadWrite,
		}

		fallbackRepo, err := fallback.New(t.Context(), []*resolverruntime.Resolver{
			{
				Repository: &repoWithComponent,
				Prefix:     "",
				Priority:   0,
			},
		}, provider, nil)
		r.NoError(err, "failed to create fallback repository")

		res, err := fallbackRepo.AddLocalSource(ctx, desc.Component.Name, desc.Component.Version, source, sourceContent)

		r.NoError(err, "Failed to add local source when it should succeed")
		r.NotNil(res, "Expected source to be not nil after adding it")
	})
}
