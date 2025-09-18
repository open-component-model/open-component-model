package v1alpha1_test

import (
	"encoding/json"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/spec"
	resolver "ocm.software/open-component-model/bindings/go/repository/component/resolver/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

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

func Test_ResolverRepository_GetRepositorySpec(t *testing.T) {
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

			res, err := resolver.NewResolverRepository(ctx, tc.repos)
			r.NoError(err, "failed to create resolver repository when it should succeed")

			identity, err := runtime.ParseURLToIdentity(tc.component)
			r.NoError(err, "failed to parse component identity when it should succeed")

			repo, err := res.GetRepositorySpec(ctx, identity)
			if tc.err(t, err, "error getting repository for component") {
				return
			} else {
				r.NoError(err, "failed to get repository spec when it should succeed")
				r.NotNil(repo, "expected non-nil repository spec")
			}
		})
	}
}
