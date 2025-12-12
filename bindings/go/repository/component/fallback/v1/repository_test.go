package v1_test

import (
	"context"
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	fallback "ocm.software/open-component-model/bindings/go/repository/component/fallback/v1"
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
					Repository: NewRepositorySpec("single-repo", nil),
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
					Repository: NewRepositorySpec("single-repo-with-prefix", nil),
					Prefix:     "prefixA",
					Priority:   0,
				},
			},
			expected: []string{"single-repo-with-prefix"},
			err:      assert.NoError,
		},
		{
			name:      "single repository with prefix matching path segment",
			component: "prefixA/component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpec("single-repo-with-prefix", nil),
					Prefix:     "prefixA",
					Priority:   0,
				},
			},
			expected: []string{"single-repo-with-prefix"},
			err:      assert.NoError,
		},
		{
			name:      "single repository with prefix matching path segment and trailing slash",
			component: "prefixA/component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpec("single-repo-with-prefix", nil),
					Prefix:     "prefixA/",
					Priority:   0,
				},
			},
			expected: []string{"single-repo-with-prefix"},
			err:      assert.NoError,
		},
		{
			name:      "single repository with prefix matching partial path segment",
			component: "prefixA/component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpec("single-repo-with-prefix", nil),
					Prefix:     "prefix",
					Priority:   0,
				},
			},
			expected: []string{},
			err:      assert.Error,
		},
		{
			name:      "multiple repositories with prefixes",
			component: "prefixB/component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpec("repoWithPrefixA", nil),
					Prefix:     "prefixA",
					Priority:   0,
				},
				{
					Repository: NewRepositorySpec("repoWithPrefixB", nil),
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
					Repository: NewRepositorySpec("repoWithPriority1", nil),
					Prefix:     "",
					Priority:   1,
				},
				{
					Repository: NewRepositorySpec("repoWithPriority2", nil),
					Prefix:     "",
					Priority:   2,
				},
				{
					Repository: NewRepositorySpec("repoWithPriority3", nil),
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
					Repository: NewRepositorySpec("repoWithPrefixA-Priority0", nil),
					Prefix:     "prefixA",
					Priority:   0,
				},
				{
					Repository: NewRepositorySpec("repoWithPrefixB-Priority0", nil),
					Prefix:     "prefixB",
					Priority:   0,
				},
				{
					Repository: NewRepositorySpec("repoWithPrefixB-Priority1", nil),
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
					Repository: NewRepositorySpec("repoWithPrefixA", nil),
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
					Repository: NewRepositorySpec("nil-repo", nil, PolicyReturnNilOnGetRepositoryForSpec),
					Prefix:     "",
					Priority:   0,
				},
			},
			expected: []string{},
			err:      assert.NoError,
		},
		{
			name:      "fail to resolve repository",
			component: "test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: NewRepositorySpec("fail-repo", nil, PolicyErrorOnGetRepositoryForSpec),
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

			fallback, err := fallback.NewFallbackRepository(ctx, MockProvider{}, nil, tc.repos)
			r.NoError(err, "failed to create fallback repository when it should succeed")

			actualRepos := fallback.RepositoriesForComponentIterator(ctx, tc.component)
			expectedRepos := make([]string, len(tc.expected))
			index := 0
			for repo, err := range actualRepos {
				if !tc.err(t, err, "unexpected error for case %s", tc.name) {
					return
				}
				if err != nil || repo == nil {
					return
				}
				expectedRepos[index] = repo.(*MockRepository).Name
				index++
			}
			r.Equal(tc.expected, expectedRepos, "expected repositories do not match actual repositories")
		})
	}
}

var MockType = runtime.NewUnversionedType("mock-repository")

const (
	PolicyErrorOnGetRepositoryForSpec     = "fail-get-repository-for-spec"
	PolicyReturnNilOnGetRepositoryForSpec = "nil-get-repository-for-spec"
)

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

func NewRepositorySpec(name string, components map[string][]string, failPolicy ...string) *RepositorySpec {
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

type MockProvider struct{}

func (m MockProvider) GetComponentVersionRepositoryScheme() *runtime.Scheme {
	//TODO implement me
	panic("implement me")
}

var _ repository.ComponentVersionRepositoryProvider = MockProvider{}

func (m MockProvider) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (m MockProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
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
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) AddLocalSource(ctx context.Context, component, version string, res *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	//TODO implement me
	panic("implement me")
}
