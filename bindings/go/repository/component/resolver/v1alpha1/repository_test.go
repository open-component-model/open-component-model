package v1alpha1_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	resolver "ocm.software/open-component-model/bindings/go/repository/component/resolver/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_GetRepositoriesForComponentIterator(t *testing.T) {
	ctx := t.Context()

	cases := []struct {
		name      string
		component string
		repos     []*resolverspec.Resolver
		expected  []string
		err       assert.ErrorAssertionFunc
	}{
		{
			name:      "test-component with one version",
			component: "test-component",
			repos: []*resolverspec.Resolver{
				{
					Repository: NewRepositorySpecRaw(t, "single-repo", map[string][]string{
						"test-component": {"1.0.0"},
					}),
					ComponentName: "test-component",
				},
			},
			expected: []string{"single-repo"},
			err:      assert.NoError,
		},
		{
			name:      "test-component with no version",
			component: "test-component",
			repos: []*resolverspec.Resolver{
				{
					Repository:    NewRepositorySpecRaw(t, "single-repo", map[string][]string{}),
					ComponentName: "test-component",
				},
			},
			expected: []string{"single-repo"},
			err:      assert.NoError,
		},
		{
			name:      "test-component with multiple repositories",
			component: "test-component",
			repos: []*resolverspec.Resolver{
				{
					Repository: NewRepositorySpecRaw(t, "repo1", map[string][]string{
						"test-component": {"1.0.0"},
					}),
					ComponentName: "test-component",
				},
				{
					Repository: NewRepositorySpecRaw(t, "repo2", map[string][]string{
						"other-component": {"1.0.0"},
					}),
					ComponentName: "repo2",
				},
				{
					Repository: NewRepositorySpecRaw(t, "repo3", map[string][]string{
						"test-component": {"2.0.0"},
					}),
					ComponentName: "test-component",
				},
			},
			expected: []string{"repo1", "repo3"},
			err:      assert.NoError,
		},
		{
			// glob component name pattern
			name:      "glob pattern match",
			component: "ocm.software/core/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:    NewRepositorySpecRaw(t, "repo-glob", map[string][]string{"ocm.software/core/test": {"1.0.0"}}),
					ComponentName: "ocm.software/core/*",
				},
			},
			expected: []string{"repo-glob"},
			err:      assert.NoError,
		},
		{
			// glob component name pattern no match
			name:      "glob pattern no match",
			component: "ocm.software/other/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:    NewRepositorySpecRaw(t, "repo-glob", map[string][]string{"ocm.software/core/test": {"1.0.0"}}),
					ComponentName: "ocm.software/core/*",
				},
			},
			expected: []string{},
			err:      assert.NoError,
		},
		{
			// error getting repository for spec
			name:      "error getting repository for spec",
			component: "test-component",
			repos: []*resolverspec.Resolver{
				{
					Repository:    NewRepositorySpecRaw(t, "repo-error", nil, PolicyErrorOnGetRepositoryForSpec),
					ComponentName: "test-component",
				},
			},
			expected: []string{},
			err: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Error(t, err, "expected error when getting repository for spec")
			},
		},
		// glob multiple wildcards
		{
			name:      "glob pattern multiple wildcards match",
			component: "ocm.software/core/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:    NewRepositorySpecRaw(t, "repo-glob-multi", map[string][]string{"ocm.software/core/test": {"1.0.0"}}),
					ComponentName: "*.software/*/test",
				},
			},
			expected: []string{"repo-glob-multi"},
			err:      assert.NoError,
		},
		{
			name:      "multiple glob results",
			component: "ocm.software/core/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:    NewRepositorySpecRaw(t, "repo-glob-1", map[string][]string{"ocm.software/core/test": {"1.0.0"}}),
					ComponentName: "ocm.software/*/test",
				},
				{
					Repository:    NewRepositorySpecRaw(t, "repo-glob-2", map[string][]string{"ocm.software/core/test": {"1.0.0"}}),
					ComponentName: "ocm.software/core/*",
				},
			},
			expected: []string{"repo-glob-1", "repo-glob-2"},
			err:      assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			res, err := resolver.NewResolverRepository(ctx, MockProvider{}, nil, tc.repos)
			r.NoError(err, "failed to create resolver repository when it should succeed")

			actualRepos := res.RepositoriesForComponentIterator(ctx, tc.component)
			var actualReposSlice []repository.ComponentVersionRepository
			actualRepoNames := make([]string, 0)

			for repo, err := range actualRepos {
				if tc.err(t, err, "error getting repository for component") {
					return
				}
				assert.NoError(t, err, "unexpected error getting repository for component")
				assert.NotNil(t, repo, "expected repository for component")

				actualReposSlice = append(actualReposSlice, repo)
				actualRepoNames = append(actualRepoNames, repo.(*MockRepository).Name)
			}

			r.Equal(tc.expected, actualRepoNames, "expected repositories do not match actual repositories")
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
		Policy:     r.Policy,
	}
}

var _ runtime.Typed = (*RepositorySpec)(nil)

func NewRepositorySpecRaw(t *testing.T, name string, components map[string][]string, failPolicy ...string) *runtime.Raw {
	repoSpec := &RepositorySpec{
		Type:       MockType,
		Name:       name,
		Components: components,
	}
	if len(failPolicy) == 1 {
		repoSpec.Policy = failPolicy[0]
	}

	j, err := json.Marshal(repoSpec)
	require.NoError(t, err)

	raw := &runtime.Raw{
		Type: MockType,
		Data: j,
	}

	return raw
}

type MockProvider struct{}

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
	case *runtime.Raw:
		var s RepositorySpec
		err := json.Unmarshal(spec.Data, &s)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal repository spec: %w", err)
		}
		switch s.Policy {
		case PolicyErrorOnGetRepositoryForSpec:
			return nil, fmt.Errorf("mock error for testing: %s", s.Policy)
		case PolicyReturnNilOnGetRepositoryForSpec:
			return nil, nil
		}
		return &MockRepository{
			RepositorySpec: &s,
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
