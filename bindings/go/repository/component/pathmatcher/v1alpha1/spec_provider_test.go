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

	rawRepo1 := &runtime.Raw{Type: runtime.Type{Name: "repo1"}}
	rawRepo2 := &runtime.Raw{Type: runtime.Type{Name: "repo2"}}
	rawRepo3 := &runtime.Raw{Type: runtime.Type{Name: "repo3"}}

	cases := []struct {
		name      string
		component string
		version   string
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
			want: rawRepo1,
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
		// version constraint tests
		{
			name:      "no constraint matches any version",
			component: "my-org/comp",
			version:   "1.0.0",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "my-org/*",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
		{
			name:      "no constraint matches empty version",
			component: "my-org/comp",
			version:   "",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "my-org/*",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
		{
			name:      "version constraint matches",
			component: "my-org/comp",
			version:   "1.5.0",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "my-org/*",
					VersionConstraint:    ">=1.0.0, <2.0.0",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
		{
			name:      "version constraint does not match",
			component: "my-org/comp",
			version:   "2.0.0",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "my-org/*",
					VersionConstraint:    ">=1.0.0, <2.0.0",
				},
			},
			want: nil,
			err: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Error(t, err, "expected error when no resolver matches")
			},
		},
		{
			name:      "version constraint with empty version skips resolver",
			component: "my-org/comp",
			version:   "",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "my-org/*",
					VersionConstraint:    ">=1.0.0",
				},
			},
			want: nil,
			err: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Error(t, err, "expected error when version is empty and constraint is set")
			},
		},
		{
			name:      "version routing: v1 to repo1, v2 to repo2",
			component: "my-org/comp",
			version:   "1.5.0",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "my-org/*",
					VersionConstraint:    ">=1.0.0, <2.0.0",
				},
				{
					Repository:           rawRepo2,
					ComponentNamePattern: "my-org/*",
					VersionConstraint:    ">=2.0.0",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
		{
			name:      "version routing: v2 to repo2",
			component: "my-org/comp",
			version:   "2.0.0",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "my-org/*",
					VersionConstraint:    ">=1.0.0, <2.0.0",
				},
				{
					Repository:           rawRepo2,
					ComponentNamePattern: "my-org/*",
					VersionConstraint:    ">=2.0.0",
				},
			},
			want: rawRepo2,
			err:  assert.NoError,
		},
		{
			name:      "caret constraint matches",
			component: "any",
			version:   "1.9.0",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "*",
					VersionConstraint:    "^1.2.0",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
		{
			name:      "tilde constraint matches",
			component: "any",
			version:   "1.2.5",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "*",
					VersionConstraint:    "~1.2.0",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
		{
			name:      "tilde constraint does not match minor bump",
			component: "any",
			version:   "1.3.0",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "*",
					VersionConstraint:    "~1.2.0",
				},
			},
			want: nil,
			err: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Error(t, err, "expected error when tilde constraint does not match")
			},
		},
		{
			name:      "invalid constraint returns error",
			component: "any",
			version:   "1.0.0",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "*",
					VersionConstraint:    "not-valid",
				},
			},
			want: nil,
			err: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Error(t, err, "expected error for invalid constraint")
			},
		},
		{
			name:      "name does not match even if constraint would",
			component: "my-org/comp",
			version:   "1.5.0",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "other/*",
					VersionConstraint:    ">=1.0.0",
				},
			},
			want: nil,
			err: func(t assert.TestingT, err error, i ...interface{}) bool {
				return assert.Error(t, err, "expected error when name does not match")
			},
		},
		{
			name:      "v-prefixed version works",
			component: "any",
			version:   "v1.5.0",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "*",
					VersionConstraint:    ">=1.0.0",
				},
			},
			want: rawRepo1,
			err:  assert.NoError,
		},
		{
			name:      "non-semver version skips constrained resolver, matches unconstrained",
			component: "my-org/comp",
			version:   "latest",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "my-org/*",
					VersionConstraint:    ">=1.0.0",
				},
				{
					Repository:           rawRepo2,
					ComponentNamePattern: "my-org/*",
				},
			},
			want: rawRepo2,
			err:  assert.NoError,
		},
		{
			name:      "empty version skips constrained resolver, matches unconstrained fallback",
			component: "my-org/comp",
			version:   "",
			repos: []*resolverspec.Resolver{
				{
					Repository:           rawRepo1,
					ComponentNamePattern: "my-org/*",
					VersionConstraint:    ">=1.0.0",
				},
				{
					Repository:           rawRepo2,
					ComponentNamePattern: "my-org/*",
				},
			},
			want: rawRepo2,
			err:  assert.NoError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := pathmatcher.NewSpecProvider(ctx, tc.repos)
			if err != nil {
				tc.err(t, err, "error creating spec provider")
				return
			}

			identity := runtime.Identity{
				descruntime.IdentityAttributeName: tc.component,
			}
			if tc.version != "" {
				identity[descruntime.IdentityAttributeVersion] = tc.version
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
