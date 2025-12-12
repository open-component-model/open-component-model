package v1alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_ResolverRepository_GetRepositorySpec(t *testing.T) {
	ctx := t.Context()

	rawRepo1 := &runtime.Raw{}
	rawRepo2 := &runtime.Raw{}
	rawRepo3 := &runtime.Raw{}

	cases := []struct {
		name      string
		component string
		repos     []*resolverspec.Resolver
		want      *runtime.Raw
		err       assert.ErrorAssertionFunc
	}{
		{
			name:      "test-component with no name",
			component: "",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "test-component",
				},
			},
			want: nil,
			err: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Error(t, err, "expected error when getting repository for spec")
			},
		},
		{
			name:      "test-component with one version",
			component: "test-component",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "test-component",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
		{
			name:      "test-component with no version",
			component: "test-component",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "test-component",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
		{
			name:      "test-component with multiple repositories",
			component: "test-component",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "test-component",
				},
				{
					Repository:           rawRepo2,
					ComponentNamePattern: "repo2",
				},
				{
					Repository:           rawRepo3,
					ComponentNamePattern: "test-component",
				},
			},
			want: rawRepo3,
			err:  assert.NoError,
		},
		{
			// glob component name pattern
			name:      "glob pattern match",
			component: "ocm.software/core/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "ocm.software/core/*",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		}, {
			// glob component name pattern
			name:      "glob pattern wildcard match",
			component: "ocm.software/core/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "ocm.software/core/negative",
				},
				{
					Repository:           rawRepo2,
					ComponentNamePattern: "*",
				},
			},
			want: rawRepo2,
			err:  assert.NoError,
		},
		{
			// glob component name pattern no match
			name:      "glob pattern no match",
			component: "ocm.software/other/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "ocm.software/core/*",
				},
			},
			want: nil,
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
					Repository:           rawRepo1,
					ComponentNamePattern: "*.software/*/test",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
		{
			name:      "multiple glob results",
			component: "ocm.software/core/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "ocm.software/*/test",
				},
				{
					Repository:           rawRepo2,
					ComponentNamePattern: "ocm.software/core/*",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := pathmatcher.NewSpecProvider(ctx, tc.repos)

			identity := runtime.Identity{
				descruntime.IdentityAttributeName: tc.component,
			}

			repo, err := res.GetRepositorySpec(ctx, identity)
			tc.err(t, err, "error getting repository for component")
			if tc.want != nil {
				assert.Equal(t, tc.want, repo, "repository spec does not match expected")
			} else {
				assert.Nil(t, repo, "expected nil repository spec")
			}
		})
	}
}
