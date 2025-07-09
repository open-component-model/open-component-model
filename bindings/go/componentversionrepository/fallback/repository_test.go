package fallback

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/componentversionrepository"
	resolverruntime "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	FailType = runtime.NewUnversionedType("fail")
	NilType  = runtime.NewUnversionedType("nil")
)

func Test_GetRepositoriesForComponentIterator(t *testing.T) {
	ctx := t.Context()

	cases := []struct {
		name      string
		component string
		repos     []*resolverruntime.Resolver
		expected  []runtime.Type
		err       assert.ErrorAssertionFunc
	}{
		{
			name:      "single repository",
			component: "test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallbackWithPriority0"),
					},
					Prefix:   "",
					Priority: 0,
				},
			},
			expected: []runtime.Type{runtime.NewUnversionedType("fallbackWithPriority0")},
			err:      assert.NoError,
		},
		{
			name:      "multiple repositories with different priorities",
			component: "test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallbackWithPriority1"),
					},
					Prefix:   "",
					Priority: 1,
				},
				{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallbackWithPriority2"),
					},
					Prefix:   "",
					Priority: 2,
				},
				{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallbackWithPriority3"),
					},
					Prefix:   "",
					Priority: 3,
				},
			},
			expected: []runtime.Type{
				runtime.NewUnversionedType("fallbackWithPriority3"),
				runtime.NewUnversionedType("fallbackWithPriority2"),
				runtime.NewUnversionedType("fallbackWithPriority1"),
			},
			err: assert.NoError,
		},
		{
			name:      "no repositories",
			component: "test-component",
			repos:     []*resolverruntime.Resolver{},
			expected:  []runtime.Type{},
			err:       assert.Error,
		},
		{
			name:      "repository with prefixes",
			component: "prefixB-test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallbackWithPrefixA"),
					},
					Prefix:   "prefixA",
					Priority: 0,
				},
				{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallbackWithPrefixB"),
					},
					Prefix:   "prefixB",
					Priority: 0,
				},
			},
			expected: []runtime.Type{
				runtime.NewUnversionedType("fallbackWithPrefixB"),
			},
			err: assert.NoError,
		},
		{
			name:      "repository with prefixes and priority",
			component: "prefixB-test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallbackWithPrefixA"),
					},
					Prefix:   "prefixA",
					Priority: 0,
				},
				{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallbackWithPrefixB-Priority0"),
					},
					Prefix:   "prefixB",
					Priority: 0,
				},
				{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallbackWithPrefixB-Priority1"),
					},
					Prefix:   "prefixB",
					Priority: 1,
				},
			},
			expected: []runtime.Type{
				runtime.NewUnversionedType("fallbackWithPrefixB-Priority1"),
				runtime.NewUnversionedType("fallbackWithPrefixB-Priority0"),
			},
			err: assert.NoError,
		},
		{
			name:      "fail to resolve repository",
			component: "test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: &runtime.Raw{
						Type: FailType,
					},
					Prefix:   "",
					Priority: 0,
				},
			},
			expected: []runtime.Type{},
			err:      assert.Error,
		},
		{
			name:      "no resolvers with matching prefix",
			component: "test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallbackWithPrefixA"),
					},
					Prefix:   "prefixA",
					Priority: 0,
				},
			},
			expected: []runtime.Type{},
			err:      assert.Error,
		},
		{
			name:      "nil repository",
			component: "test-component",
			repos: []*resolverruntime.Resolver{
				{
					Repository: &runtime.Raw{
						Type: NilType,
					},
					Prefix:   "",
					Priority: 0,
				},
			},
			expected: []runtime.Type{},
			err:      assert.Error,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			fallback, err := NewFallbackRepository(ctx, MockProvider{}, nil, tc.repos...)
			r.NoError(err, "failed to create fallback repository when it should succeed")

			actualRepos := fallback.RepositoriesForComponentIterator(ctx, "test-component")
			expectedRepos := make([]runtime.Type, len(tc.expected))
			index := 0
			for repo, err := range actualRepos {
				if !tc.err(t, err, "unexpected error for case %s", tc.name) {
					return
				}
				if err != nil && repo == nil {
					return
				}
				expectedRepos[index] = repo.(*MockRepository).Type()
				index++
			}
		})
	}
}

type MockProvider struct{}

func (m MockProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (m MockProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (componentversionrepository.ComponentVersionRepository, error) {
	if repositorySpecification.GetType() == FailType {
		return nil, fmt.Errorf("mock error for testing")
	}
	if repositorySpecification.GetType() == NilType {
		return nil, nil
	}
	return &MockRepository{}, nil
}

type MockRepository struct {
	typ runtime.Type
}

func (m MockRepository) Type() runtime.Type {
	return m.typ
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
