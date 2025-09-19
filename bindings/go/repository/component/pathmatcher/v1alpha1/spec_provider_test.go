package v1alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_ResolverRepository_GetRepositorySpec(t *testing.T) {
	ctx := t.Context()

	cases := []struct {
		name            string
		component       string
		repos           []*resolverspec.Resolver
		shouldReturnRep bool
		err             assert.ErrorAssertionFunc
	}{
		{
			name:      "test-component with no name",
			component: "",
			repos: []*resolverspec.Resolver{
				{
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "test-component",
				},
			},
			shouldReturnRep: false,
			err: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Error(t, err, "expected error when getting repository for spec")
			},
		},
		{
			name:      "test-component with one version",
			component: "test-component",
			repos: []*resolverspec.Resolver{
				{
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "test-component",
				},
			},
			shouldReturnRep: true,
			err:             assert.NoError,
		},
		{
			name:      "test-component with no version",
			component: "test-component",
			repos: []*resolverspec.Resolver{
				{
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "test-component",
				},
			},
			shouldReturnRep: true,
			err:             assert.NoError,
		},
		{
			name:      "test-component with multiple repositories",
			component: "test-component",
			repos: []*resolverspec.Resolver{
				{
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "test-component",
				},
				{
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "repo2",
				},
				{
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "test-component",
				},
			},
			shouldReturnRep: true,
			err:             assert.NoError,
		},
		{
			// glob component name pattern
			name:      "glob pattern match",
			component: "ocm.software/core/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "ocm.software/core/*",
				},
			},
			shouldReturnRep: true,
			err:             assert.NoError,
		},
		{
			// glob component name pattern no match
			name:      "glob pattern no match",
			component: "ocm.software/other/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "ocm.software/core/*",
				},
			},
			shouldReturnRep: false,
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
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "*.software/*/test",
				},
			},
			shouldReturnRep: true,
			err:             assert.NoError,
		},
		{
			name:      "multiple glob results",
			component: "ocm.software/core/test",
			repos: []*resolverspec.Resolver{
				{
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "ocm.software/*/test",
				},
				{
					Repository:           &runtime.Raw{},
					ComponentNamePattern: "ocm.software/core/*",
				},
			},
			shouldReturnRep: true,
			err:             assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			res := pathmatcher.NewSpecProvider(ctx, tc.repos)

			identity := runtime.Identity{
				descruntime.IdentityAttributeName: tc.component,
			}

			repo, err := res.GetRepositorySpec(ctx, identity)
			tc.err(t, err, "error getting repository for component")
			if tc.shouldReturnRep {
				r.NotNil(repo, "expected non-nil repository spec")
			} else {
				assert.Nil(t, repo, "expected nil repository spec")
			}
		})
	}
}
