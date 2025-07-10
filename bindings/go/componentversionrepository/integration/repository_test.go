package integration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/componentversionrepository/fallback"
	resolverruntime "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/runtime"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ociprovider "ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	helloWorldComponentName    = "github.com/acme.org/helloworld"
	notHelloWorldComponentName = "github.com/acme.org/not-helloworld"
	componentVersion           = "1.0.0"
	resourceName               = "resource"
	resourceVersion            = "6.7.1"
	sourceName                 = "source"
	sourceVersion              = "6.7.1"
)

func Test_GetComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	primaryRepo, primarySpecWithHelloWorldv1 := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())
	r.NoError(primaryRepo.AddComponentVersion(ctx, createTestComponent(t, helloWorldComponentName, componentVersion)))

	secondaryRepo, secondarySpecWithHelloWorldv1 := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())
	r.NoError(secondaryRepo.AddComponentVersion(ctx, createTestComponent(t, helloWorldComponentName, componentVersion)))

	tertiaryRepo, specWithNotHelloWorldv1 := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())
	r.NoError(tertiaryRepo.AddComponentVersion(ctx, createTestComponent(t, notHelloWorldComponentName, componentVersion)))

	nonExistingRepoSpec := &ctfv1.Repository{
		Path:       "non-existing-repo-path",
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
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: primarySpecWithHelloWorldv1,
			err:          assert.NoError,
		},
		{
			name:      "found with fallback",
			component: helloWorldComponentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: specWithNotHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: primarySpecWithHelloWorldv1,
			err:          assert.NoError,
		},
		{
			name:      "higher priority first",
			component: helloWorldComponentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: secondarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   20,
				},
			},
			expectedRepo: primarySpecWithHelloWorldv1,
			err:          assert.NoError,
		},
		{
			name:      "same priority, first in list first",
			component: helloWorldComponentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: secondarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: secondarySpecWithHelloWorldv1,
			err:          assert.NoError,
		},
		{
			name:      "prefix matched",
			component: helloWorldComponentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: secondarySpecWithHelloWorldv1,
					Prefix:     "github.com/not-acme.org",
					Priority:   0,
				},
				{
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "github.com/acme.org",
					Priority:   0,
				},
			},
			expectedRepo: primarySpecWithHelloWorldv1,
			err:          assert.NoError,
		},
		{
			name:      "partial prefix matched",
			component: helloWorldComponentName,
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "github.com/ac",
					Priority:   0,
				},
			},
			expectedRepo: primarySpecWithHelloWorldv1,
			err:          assert.NoError,
		},
		{
			name:      "not found with fallback",
			component: "github.com/not-acme.org/non-existing-component",
			version:   componentVersion,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: secondarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: specWithNotHelloWorldv1,
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
					Repository: nonExistingRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: primarySpecWithHelloWorldv1,
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
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: nonExistingRepoSpec,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: primarySpecWithHelloWorldv1,
			err:          assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logBuffer bytes.Buffer
			logHandler := slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})
			logger := slog.New(logHandler)
			slog.SetDefault(logger)

			provider := ociprovider.NewComponentVersionRepositoryProvider()

			fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), provider, nil, tc.resolvers...)
			r.NoError(err)

			desc, err := fallbackRepo.GetComponentVersion(ctx, tc.component, tc.version)
			if !tc.err(t, err) {
				return
			}
			if err != nil && tc.expectedRepo == nil {
				return
			}
			r.NotNil(desc, "Expected descriptor to be not nil for component %s and version %s", tc.component, tc.version)
			r.Equal(tc.component, desc.Component.Name)
			r.Equal(tc.version, desc.Component.Version)

			expectedRepoData, err := json.Marshal(tc.expectedRepo)
			r.NoError(err, "Failed to marshal expected repository")

			usedRepoData, err := extractUsedRepoFromLogs(logBuffer)
			r.NoError(err, "Failed to marshal used repository")

			r.YAMLEq(string(expectedRepoData), string(usedRepoData))
		})
	}
}

func Test_ListComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	primaryRepo, primarySpecWithHelloWorldv1 := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())
	r.NoError(primaryRepo.AddComponentVersion(ctx, createTestComponent(t, helloWorldComponentName, componentVersion)))

	secondaryRepo, secondarySpecWithHelloWorldv1 := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())
	r.NoError(secondaryRepo.AddComponentVersion(ctx, createTestComponent(t, helloWorldComponentName, componentVersion)))

	tertiaryRepo, specWithNotHelloWorldv1AndHelloWorldv2 := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())
	r.NoError(tertiaryRepo.AddComponentVersion(ctx, createTestComponent(t, notHelloWorldComponentName, componentVersion)))
	r.NoError(tertiaryRepo.AddComponentVersion(ctx, createTestComponent(t, helloWorldComponentName, "2.0.0")))

	nonExistingRepoSpec := &ctfv1.Repository{
		Path:       "non-existing-repo-path",
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
					Repository: primarySpecWithHelloWorldv1,
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
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: secondarySpecWithHelloWorldv1,
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
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: specWithNotHelloWorldv1AndHelloWorldv2,
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
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: specWithNotHelloWorldv1AndHelloWorldv2,
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
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "github.com/not-acme.org",
					Priority:   20,
				},
				{
					Repository: specWithNotHelloWorldv1AndHelloWorldv2,
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
					Repository: nonExistingRepoSpec,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: specWithNotHelloWorldv1AndHelloWorldv2,
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
					Repository: specWithNotHelloWorldv1AndHelloWorldv2,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: nonExistingRepoSpec,
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
					Repository: primarySpecWithHelloWorldv1,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: specWithNotHelloWorldv1AndHelloWorldv2,
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
			provider := ociprovider.NewComponentVersionRepositoryProvider()

			fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), provider, nil, tc.resolvers...)
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

	_, specWithComponent := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())
	desc := createTestComponent(t, helloWorldComponentName, componentVersion)

	_, specWithoutComponent := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())

	cases := []struct {
		name         string
		descriptor   *descriptor.Descriptor
		resolvers    []*resolverruntime.Resolver
		expectedRepo runtime.Typed
		err          assert.ErrorAssertionFunc
	}{
		{
			name:       "add component version without fallback",
			descriptor: desc,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: specWithComponent,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: specWithComponent,
			err:          assert.NoError,
		},
		{
			name:       "add component version with fallback",
			descriptor: desc,
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: specWithoutComponent,
					Prefix:     "do-not-match",
					Priority:   20,
				},
				{
					Repository: specWithComponent,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: specWithComponent,
			err:          assert.NoError,
		},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logBuffer bytes.Buffer
			logHandler := slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})
			logger := slog.New(logHandler)
			slog.SetDefault(logger)

			// adjust component name to avoid conflicts in the same test run
			tc.descriptor.Component.Name = fmt.Sprintf("%s-%d", tc.descriptor.Component.Name, index)

			provider := ociprovider.NewComponentVersionRepositoryProvider()

			fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), provider, nil, tc.resolvers...)
			r.NoError(err, "failed to create fallback repository")

			_, err = fallbackRepo.GetComponentVersion(ctx, tc.descriptor.Component.Name, tc.descriptor.Component.Version)
			r.Error(err)

			// Test adding component version
			err = fallbackRepo.AddComponentVersion(ctx, tc.descriptor)
			r.NoError(err, "Failed to add component version when it should succeed")

			usedRepoData, err := extractUsedRepoFromLogs(logBuffer)
			r.NoError(err, "Failed to extract used repository from logs")

			expectedRepoData, err := json.Marshal(tc.expectedRepo)
			r.NoError(err, "Failed to marshal expected repository")

			r.YAMLEq(string(expectedRepoData), string(usedRepoData), "Expected used repository to match expected repository")

			err = fallbackRepo.AddComponentVersion(ctx, tc.descriptor)
			r.NoError(err, "Failed to add component version when it should succeed")

			desc, err := fallbackRepo.GetComponentVersion(ctx, tc.descriptor.Component.Name, tc.descriptor.Component.Version)
			r.NoError(err, "Failed to get component version after adding it")

			r.NotNil(desc, "Component version should not be nil after adding it")
			r.Equal(tc.descriptor.Component.Name, desc.Component.Name, "Component name should match")
		})
	}
}

func Test_GetLocalResource(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	repoWithResource, specWithResource := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())
	component := createTestComponent(t, helloWorldComponentName, componentVersion)
	resource, data := createTestLocalResource(t, resourceName, resourceVersion, "some content")
	component.Component.Resources = append(component.Component.Resources, *resource)
	_, err := repoWithResource.AddLocalResource(ctx, helloWorldComponentName, componentVersion, resource, data)
	r.NoError(repoWithResource.AddComponentVersion(ctx, component))
	r.NoError(err)

	_, specWithoutResource := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())

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
					Repository: specWithResource,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: specWithResource,
			err:          assert.NoError,
		},
		{
			name:      "found with fallback",
			component: helloWorldComponentName,
			version:   componentVersion,
			resourceIdentity: map[string]string{
				descriptor.IdentityAttributeName:    resourceName,
				descriptor.IdentityAttributeVersion: resourceVersion,
			},
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: specWithoutResource,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: specWithResource,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: specWithResource,
			err:          assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logBuffer bytes.Buffer
			logHandler := slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{
				Level: slog.LevelDebug,
			})
			logger := slog.New(logHandler)
			slog.SetDefault(logger)

			provider := ociprovider.NewComponentVersionRepositoryProvider()
			fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), provider, nil, tc.resolvers...)
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

			expectedRepoData, err := json.Marshal(tc.expectedRepo)
			r.NoError(err, "Failed to marshal expected repository")
			usedRepoData, err := extractUsedRepoFromLogs(logBuffer)
			r.YAMLEq(string(expectedRepoData), string(usedRepoData), "Expected used repository to match expected repository")
		})
	}
}

func Test_GetLocalSource(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	repoWithResource, specWithSource := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())
	component := createTestComponent(t, helloWorldComponentName, componentVersion)
	source, data := createTestLocalSource(t, sourceName, sourceVersion, "some content")
	component.Component.Sources = append(component.Component.Sources, *source)
	_, err := repoWithResource.AddLocalSource(ctx, helloWorldComponentName, componentVersion, source, data)
	r.NoError(repoWithResource.AddComponentVersion(ctx, component))
	r.NoError(err)

	_, specWithoutSource := createTempCTFBasedOCIRepositoryAndSpec(t, t.TempDir())

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
					Repository: specWithSource,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: specWithSource,
			err:          assert.NoError,
		},
		{
			name:      "found with fallback",
			component: helloWorldComponentName,
			version:   componentVersion,
			resourceIdentity: map[string]string{
				descriptor.IdentityAttributeName:    sourceName,
				descriptor.IdentityAttributeVersion: sourceVersion,
			},
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: specWithoutSource,
					Prefix:     "",
					Priority:   20,
				},
				{
					Repository: specWithSource,
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedRepo: specWithSource,
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
			fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), provider, nil, tc.resolvers...)
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

			expectedRepoData, err := json.Marshal(tc.expectedRepo)
			r.NoError(err, "Failed to marshal expected repository")
			usedRepoData, err := extractUsedRepoFromLogs(logBuffer)
			r.YAMLEq(string(expectedRepoData), string(usedRepoData), "Expected used repository to match expected repository")
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
		Access: &v2.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			LocalReference: digest.FromString(resourceContentString).String(),
			MediaType:      "my-media-type",
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

		fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), provider, nil, &resolverruntime.Resolver{
			Repository: &repoWithComponent,
			Prefix:     "",
			Priority:   0,
		})
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
		Access: &v2.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			LocalReference: digest.FromString(sourceContentString).String(),
			MediaType:      "my-media-type",
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

		fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), provider, nil, &resolverruntime.Resolver{
			Repository: &repoWithComponent,
			Prefix:     "",
			Priority:   0,
		})
		r.NoError(err, "failed to create fallback repository")

		res, err := fallbackRepo.AddLocalSource(ctx, desc.Component.Name, desc.Component.Version, source, sourceContent)

		r.NoError(err, "Failed to add local source when it should succeed")
		r.NotNil(res, "Expected source to be not nil after adding it")
	})
}

func createTempCTFBasedOCIRepositoryAndSpec(t *testing.T, path string) (*oci.Repository, runtime.Typed) {
	r := require.New(t)
	fs, err := filesystem.NewFS(path, os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(ocictf.WithCTF(store))
	r.NoError(err, "Failed to create OCI repository from CTF store")

	spec := &ctfv1.Repository{
		Path:       path,
		AccessMode: ctfv1.AccessModeReadWrite,
	}
	return repo, spec
}

func createTestComponent(t *testing.T, name, version string) *descriptor.Descriptor {
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			Provider: descriptor.Provider{
				Name: "test-provider",
			},
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
		},
	}
	return desc
}

func createTestLocalResource(t *testing.T, name, version, content string) (*descriptor.Resource, blob.ReadOnlyBlob) {
	resourceContent := inmemory.New(bytes.NewReader([]byte(content)))
	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    name,
				Version: version,
			},
		},
		Type: "my-type",
		Access: &v2.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			LocalReference: digest.FromString(content).String(),
			MediaType:      "my-media-type",
		},
		Digest:       nil,
		CreationTime: descriptor.CreationTime{},
	}
	return resource, resourceContent
}

func createTestLocalSource(t *testing.T, name, version, content string) (*descriptor.Source, blob.ReadOnlyBlob) {
	sourceContent := inmemory.New(bytes.NewReader([]byte(content)))
	source := &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    name,
				Version: version,
			},
		},
		Type: "my-type",
		Access: &v2.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			LocalReference: digest.FromString(content).String(),
			MediaType:      "my-media-type",
		},
	}
	return source, sourceContent
}

func extractUsedRepoFromLogs(logBuffer bytes.Buffer) ([]byte, error) {
	var usedRepo map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(logBuffer.Bytes()))
	for scanner.Scan() {
		line := scanner.Bytes()
		var logs map[string]any
		if err := json.Unmarshal(line, &logs); err != nil {
			return nil, fmt.Errorf("failed to unmarshal log line: %w", err)
		}
		if strings.Contains(logs["msg"].(string), "yielding repository for component") {
			usedRepo = logs["repository"].(map[string]any)
		}
	}
	usedRepoData, err := json.Marshal(usedRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal used repository: %w", err)
	}
	return usedRepoData, nil
}
