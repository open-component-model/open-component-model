package v1alpha1_test

import (
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
					Repository:    &runtime.Raw{},
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
					Repository:    &runtime.Raw{},
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
					Repository:    &runtime.Raw{},
					ComponentName: "test-component",
				},
				{
					Repository:    &runtime.Raw{},
					ComponentName: "repo2",
				},
				{
					Repository:    &runtime.Raw{},
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
					Repository:    &runtime.Raw{},
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
					Repository:    &runtime.Raw{},
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
					Repository:    &runtime.Raw{},
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
					Repository:    &runtime.Raw{},
					ComponentName: "ocm.software/*/test",
				},
				{
					Repository:    &runtime.Raw{},
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

			identity := runtime.Identity{
				resolver.IdentityKey: tc.component,
			}

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
