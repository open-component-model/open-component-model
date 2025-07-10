package fallback_test

import (
	"context"
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/componentversionrepository/fallback"
	resolverruntime "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_GetRepositoriesForComponentIterator(t *testing.T) {
	ctx := t.Context()

	cases := []struct {
		name      string
		component string
		repos     []*resolverruntime.Resolver
		expected  []string
		err       assert.ErrorAssertionFunc
	}{
		{
			name:      "single repository",
			component: "test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("single-repo", nil),
					Prefix:     "",
					Priority:   0,
				},
			},
			expected: []string{"single-repo"},
			err:      assert.NoError,
		},
		{
			name:      "single repository with prefix equal component name",
			component: "prefixA",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("single-repo-with-prefix", nil),
					Prefix:     "prefixA",
					Priority:   0,
				},
			},
			expected: []string{"single-repo-with-prefix"},
			err:      assert.NoError,
		},
		{
			name:      "single repository with prefix",
			component: "prefixA/component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("single-repo-with-prefix", nil),
					Prefix:     "prefixA",
					Priority:   0,
				},
			},
			expected: []string{"single-repo-with-prefix"},
			err:      assert.NoError,
		},
		{
			name:      "multiple repositories with prefixes",
			component: "prefixB/component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("repoWithPrefixA", nil),
					Prefix:     "prefixA",
					Priority:   0,
				},
				{
					Repository: NewRepositorySpecWithComponents("repoWithPrefixB", nil),
					Prefix:     "prefixB",
					Priority:   0,
				},
			},
			expected: []string{
				"repoWithPrefixB",
			},
			err: assert.NoError,
		},
		{
			name:      "multiple repositories with different priorities",
			component: "test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("repoWithPriority1", nil),
					Prefix:     "",
					Priority:   1,
				},
				{
					Repository: NewRepositorySpecWithComponents("repoWithPriority2", nil),
					Prefix:     "",
					Priority:   2,
				},
				{
					Repository: NewRepositorySpecWithComponents("repoWithPriority3", nil),
					Prefix:     "",
					Priority:   3,
				},
			},
			expected: []string{
				"repoWithPriority3",
				"repoWithPriority2",
				"repoWithPriority1",
			},
			err: assert.NoError,
		},
		{
			name:      "multiple repositories with prefixes and priority",
			component: "prefixB/component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("repoWithPrefixA-Priority0", nil),
					Prefix:     "prefixA",
					Priority:   0,
				},
				{
					Repository: NewRepositorySpecWithComponents("repoWithPrefixB-Priority0", nil),
					Prefix:     "prefixB",
					Priority:   0,
				},
				{
					Repository: NewRepositorySpecWithComponents("repoWithPrefixB-Priority1", nil),
					Prefix:     "prefixB",
					Priority:   1,
				},
			},
			expected: []string{
				"repoWithPrefixB-Priority1",
				"repoWithPrefixB-Priority0",
			},
			err: assert.NoError,
		},
		{
			name:      "no resolvers with matching prefix",
			component: "prefixB/component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("repoWithPrefixA", nil),
					Prefix:     "prefixA",
					Priority:   0,
				},
			},
			expected: []string{},
			err:      assert.Error,
		},
		{
			name:      "nil repository",
			component: "test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("nil-repo", nil, PolicyReturnNilOnGetRepositoryForSpec),
					Prefix:     "",
					Priority:   0,
				},
			},
			expected: []string{},
			err:      assert.Error,
		},
		{
			name:      "fail to resolve repository",
			component: "test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("fail-repo", nil, PolicyErrorOnGetRepositoryForSpec),
					Prefix:     "",
					Priority:   0,
				},
			},
			expected: []string{},
			err:      assert.Error,
		},
		{
			name:      "no repositories",
			component: "test-component",
			repos:     []*resolverruntime.Resolver{},
			expected:  []string{},
			err:       assert.Error,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			fallback, err := fallback.NewFallbackRepository(ctx, MockProvider{}, nil, tc.repos...)
			r.NoError(err, "failed to create fallback repository when it should succeed")

			actualRepos := fallback.RepositoriesForComponentIterator(ctx, tc.component)
			expectedRepos := make([]string, len(tc.expected))
			index := 0
			for repo, err := range actualRepos {
				if !tc.err(t, err, "unexpected error for case %s", tc.name) {
					return
				}
				if err != nil && repo == nil {
					return
				}
				expectedRepos[index] = repo.(*MockRepository).Name
				index++
			}
			r.Equal(tc.expected, expectedRepos, "expected repositories do not match actual repositories")
		})
	}
}

func Test_GetComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	cases := []struct {
		name      string
		component string
		version   string
		resolvers []*resolverruntime.Resolver
		err       assert.ErrorAssertionFunc
	}{
		{
			name:      "found without fallback",
			component: "test-component",
			version:   "1.0.0",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("single-repository", map[string][]string{
						"test-component": {"1.0.0"},
					}),
					Prefix:   "",
					Priority: 0,
				},
			},
			err: assert.NoError,
		},
		{
			name:      "found with fallback",
			component: "test-component",
			version:   "1.0.0",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("repository-without-component", map[string][]string{}),
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: NewRepositorySpecWithComponents("repository-with-component", map[string][]string{
						"test-component": {"1.0.0"},
					}),
					Prefix:   "",
					Priority: 0,
				},
			},
			err: assert.NoError,
		},
		{
			name:      "not found",
			component: "test-component",
			version:   "1.0.0",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("repository-without-component", map[string][]string{}),
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: NewRepositorySpecWithComponents("repository-with-component", map[string][]string{}),
					Prefix:     "",
					Priority:   0,
				},
			},
			err: assert.Error,
		},
		{
			name:      "fail on get repository",
			component: "test-component",
			version:   "1.0.0",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("fail-repository", nil, PolicyErrorOnGetRepositoryForSpec),
					Prefix:     "",
					Priority:   0,
				},
			},
			err: assert.Error,
		},
		{
			name:      "fail on get component version",
			component: "test-component",
			version:   "1.0.0",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("fail-repository", nil, PolicyFailOnGetComponentVersion),
					Prefix:     "",
					Priority:   0,
				},
			},
			err: assert.Error,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), &MockProvider{}, nil, tc.resolvers...)
			r.NoError(err)

			desc, err := fallbackRepo.GetComponentVersion(ctx, tc.component, tc.version)
			if !tc.err(t, err) {
				return
			}
			if err != nil {
				return
			}
			r.Equal(tc.component, desc.Component.Name)
			r.Equal(tc.version, desc.Component.Version)
		})
	}
}

func Test_ListComponentVersion(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	cases := []struct {
		name             string
		component        string
		resolvers        []*resolverruntime.Resolver
		expectedVersions []string
		err              assert.ErrorAssertionFunc
	}{
		{
			name:      "list versions from single repository",
			component: "test-component",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("single-repo", map[string][]string{
						"test-component": {"1.0.0", "2.0.0"},
					}),
					Prefix:   "",
					Priority: 0,
				},
			},
			expectedVersions: []string{"1.0.0", "2.0.0"},
			err:              assert.NoError,
		},
		{
			name:      "list versions from multiple repositories",
			component: "test-component",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("repo-1", map[string][]string{
						"test-component": {"1.0.0"},
					}),
					Prefix:   "",
					Priority: 0,
				},
				{
					Repository: NewRepositorySpecWithComponents("repo-2", map[string][]string{
						"test-component": {"2.0.0"},
					}),
					Prefix:   "",
					Priority: 0,
				},
			},
			expectedVersions: []string{"1.0.0", "2.0.0"},
			err:              assert.NoError,
		},
		{
			name:      "deduplicate versions found in multiple repositories",
			component: "test-component",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("repo-1", map[string][]string{
						"test-component": {"1.0.0", "2.0.0"},
					}),
					Prefix:   "",
					Priority: 0,
				},
				{
					Repository: NewRepositorySpecWithComponents("repo-2", map[string][]string{
						"test-component": {"2.0.0", "3.0.0"},
					}),
					Prefix:   "",
					Priority: 0,
				},
			},
			expectedVersions: []string{"1.0.0", "2.0.0", "3.0.0"},
			err:              assert.NoError,
		},
		{
			name:      "no versions found in multiple repositories",
			component: "test-component",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("repo-1", map[string][]string{}),
					Prefix:     "",
					Priority:   0,
				},
				{
					Repository: NewRepositorySpecWithComponents("repo-2", map[string][]string{}),
					Prefix:     "",
					Priority:   0,
				},
			},
			expectedVersions: nil,
			err:              assert.NoError,
		},
		{
			name:      "fail on get repository",
			component: "test-component",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("fail-repository", nil, PolicyErrorOnGetRepositoryForSpec),
					Prefix:     "",
					Priority:   0,
				},
			},
			err: assert.Error,
		},
		{
			name:      "fail on get component version",
			component: "test-component",
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("fail-repository", nil, PolicyFailOnGetComponentVersion),
					Prefix:     "",
					Priority:   0,
				},
			},
			err: assert.Error,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), &MockProvider{}, nil, tc.resolvers...)
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

	cases := []struct {
		name         string
		descriptor   *descriptor.Descriptor
		resolvers    []*resolverruntime.Resolver
		expectedRepo runtime.Typed
		err          assert.ErrorAssertionFunc
	}{
		{
			name:       "add component version without fallback",
			descriptor: newComponentVersion("test-component", "1.0.0"),
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("single-repo", map[string][]string{}),
					Prefix:     "",
					Priority:   0,
				},
			},
			err: assert.NoError,
		},
		{
			name:       "add component version without fallback",
			descriptor: newComponentVersion("test-component", "1.0.0"),
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("single-repo", map[string][]string{}),
					Prefix:     "",
					Priority:   0,
				},
			},
			err: assert.NoError,
		},
		{
			name:       "add component version without fallback",
			descriptor: newComponentVersion("test-component", "1.0.0"),
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithComponents("single-repo", map[string][]string{}, PolicyErrorOnGetRepositoryForSpec),
					Prefix:     "",
					Priority:   0,
				},
			},
			err: assert.Error,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), MockProvider{}, nil, tc.resolvers...)
			r.NoError(err, "failed to create fallback repository")

			err = fallbackRepo.AddComponentVersion(ctx, tc.descriptor)
			if !tc.err(t, err, "failed adding component version when it should have succeeded") {
				return
			}
		})
	}
}

func Test_GetLocalResource(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	cases := []struct {
		name      string
		component string
		version   string
		identity  map[string]string
		resolvers []*resolverruntime.Resolver
		err       assert.ErrorAssertionFunc
	}{
		{
			name:      "found without fallback",
			component: "test-component",
			version:   "1.0.0",
			identity: map[string]string{
				"name":    "resource",
				"version": "1.0.0",
			},
			resolvers: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpecWithResources("single-repository", map[string]map[string]string{
						"test-component:1.0.0": {
							"name":    "resource",
							"version": "1.0.0",
						},
					}),
					Prefix:   "",
					Priority: 0,
				},
			},
			err: assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fallbackRepo, err := fallback.NewFallbackRepository(t.Context(), &MockProvider{}, nil, tc.resolvers...)
			r.NoError(err)

			_, res, err := fallbackRepo.GetLocalResource(ctx, tc.component, tc.version, tc.identity)
			if !tc.err(t, err) {
				return
			}
			if err != nil {
				return
			}
			r.EqualValues(tc.identity, res.ToIdentity())
		})
	}
}

var MockType = runtime.NewUnversionedType("mock-repository")

const (
	PolicyErrorOnGetRepositoryForSpec     = "fail-get-repository-for-spec"
	PolicyReturnNilOnGetRepositoryForSpec = "nil-get-repository-for-spec"
	PolicyFailOnGetComponentVersion       = "fail-get-component-version"
)

// LocalBlob describes the access for a local blob.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type RepositorySpec struct {
	Type runtime.Type `json:"type"`

	// Name is used for identification of the mock repository.
	Name string

	// Components is a map of component names to a list of component versions
	// that are available in this mock repository.
	Components map[string][]string

	// Resources is a map of component:version to a list of resource identities
	// of resources that are available in this mock repository.
	Resources map[string]map[string]string

	// Policy defines additional behavior of the mock repository.
	Policy string
}

func (r *RepositorySpec) GetType() runtime.Type {
	return r.Type
}

func (r *RepositorySpec) SetType(t runtime.Type) {
	r.Type = t
}

func (r *RepositorySpec) DeepCopyTyped() runtime.Typed {
	return &RepositorySpec{
		Type:       r.Type,
		Name:       r.Name,
		Components: maps.Clone(r.Components),
		Resources:  maps.Clone(r.Resources),
		Policy:     r.Policy,
	}
}

var _ runtime.Typed = (*RepositorySpec)(nil)

func NewRepositorySpecWithComponents(name string, components map[string][]string, failPolicy ...string) *RepositorySpec {
	spec := RepositorySpec{
		Type:       MockType,
		Name:       name,
		Components: components,
	}
	if len(failPolicy) == 1 {
		spec.Policy = failPolicy[0]
	}
	return &spec
}

func NewRepositorySpecWithResources(name string, resources map[string]map[string]string, failPolicy ...string) *RepositorySpec {
	spec := RepositorySpec{
		Type:      MockType,
		Name:      name,
		Resources: resources,
	}
	if len(failPolicy) == 1 {
		spec.Policy = failPolicy[0]
	}
	return &spec
}

type MockProvider struct{}

func (m MockProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (m MockProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (componentversionrepository.ComponentVersionRepository, error) {
	switch spec := repositorySpecification.(type) {
	case *RepositorySpec:
		switch spec.Policy {
		case PolicyErrorOnGetRepositoryForSpec:
			return nil, fmt.Errorf("mock error for testing: %s", spec.Policy)
		case PolicyReturnNilOnGetRepositoryForSpec:
			return nil, nil
		}
		return &MockRepository{
			RepositorySpec: spec,
		}, nil
	default:
		panic(fmt.Sprintf("unexpected repository specification type: %T", repositorySpecification))
	}
}

type MockRepository struct {
	typ runtime.Type
	*RepositorySpec
}

func (m MockRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	m.Components[descriptor.Component.Name] = append(m.Components[descriptor.Component.Name], descriptor.Component.Version)
	return nil
}

func (m MockRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	if _, ok := m.Components[component]; !ok {
		return nil, componentversionrepository.NewErrNotFound(fmt.Sprintf("component version %s/%s not found in repository", component, version), nil)
	}
	if m.Policy == PolicyFailOnGetComponentVersion {
		return nil, fmt.Errorf("not a not found error")
	}
	return &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    component,
					Version: version,
				},
			},
		},
	}, nil
}

func (m MockRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	if m.Policy == PolicyFailOnGetComponentVersion {
		return nil, fmt.Errorf("not a not found error")
	}
	return m.Components[component], nil
}

func (m MockRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	if m.Resources[fmt.Sprintf("%s:%s", component, version)] == nil {
		return nil, nil, componentversionrepository.NewErrNotFound(fmt.Sprintf("resource %s:%s not found in repository", component, version), nil)
	}
	resource := m.Resources[fmt.Sprintf("%s:%s", component, version)]
	if !maps.Equal(resource, identity) {
		return nil, nil, componentversionrepository.NewErrNotFound(fmt.Sprintf("resource %s:%s not found in repository", component, version), nil)
	}
	id := maps.Clone(identity)
	name := id["name"]
	vers := id["version"]
	delete(id, "name")
	delete(id, "version")
	return nil, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    name,
				Version: vers,
			},
			ExtraIdentity: id,
		},
	}, nil
}

func (m MockRepository) AddLocalSource(ctx context.Context, component, version string, res *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	//TODO implement me
	panic("implement me")
}

func newResourceVersion(resource, version string) *descriptor.Resource {
	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{
				Name:    resource,
				Version: version,
			},
		},
	}
}

func newComponentVersion(component, version string) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    component,
					Version: version,
				},
			},
		},
	}
}
