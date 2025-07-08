package fallback

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	repository "ocm.software/open-component-model/bindings/go/componentversionrepository"
	resolverruntime "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"oras.land/oras-go/v2/errdef"
)

func Test_AddRepositories(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fallback, err := New(ctx, []*resolverruntime.Resolver{
		{
			Repository: &runtime.Raw{
				Type: runtime.NewUnversionedType("fallback1"),
			},
			Prefix:   "",
			Priority: 1,
		},
		{
			Repository: &runtime.Raw{
				Type: runtime.NewUnversionedType("fallback3"),
			},
			Prefix:   "",
			Priority: 3,
		},
		{
			Repository: &runtime.Raw{
				Type: runtime.NewUnversionedType("fallback0"),
			},
			Prefix:   "",
			Priority: 0,
		},
	}, nil, nil)
	r.NoError(err, "failed to create fallback repository when it should succeed")

	actualRepos := fallback.fallbackRepositories
	expectedRepos := []*repositoryWithResolverRules{
		{
			resolver: &resolverruntime.Resolver{
				Repository: &runtime.Raw{
					Type: runtime.NewUnversionedType("fallback3"),
				},
				Prefix:   "",
				Priority: 3,
			},
			repository: nil,
		},
		{
			resolver: &resolverruntime.Resolver{
				Repository: &runtime.Raw{
					Type: runtime.NewUnversionedType("fallback1"),
				},
				Prefix:   "",
				Priority: 1,
			},
			repository: nil,
		},
		{
			resolver: &resolverruntime.Resolver{
				Repository: &runtime.Raw{
					Type: runtime.NewUnversionedType("fallback0"),
				},
				Prefix:   "",
				Priority: 0,
			},
			repository: nil,
		},
	}

	r.Equal(expectedRepos, actualRepos, "repositories do not match expected repositories")

	// Test adding a repository with a higher priority

	err = fallback.AddRepositories(&resolverruntime.Resolver{
		Repository: &runtime.Raw{
			Type: runtime.NewUnversionedType("fallback5"),
		},
		Prefix:   "",
		Priority: 5,
	})
	r.NoError(err, "failed to add repository when it should succeed")

	actualRepos = fallback.fallbackRepositories
	expectedRepos = slices.Insert(expectedRepos, 0, &repositoryWithResolverRules{
		resolver: &resolverruntime.Resolver{
			Repository: &runtime.Raw{
				Type: runtime.NewUnversionedType("fallback5"),
			},
			Prefix:   "",
			Priority: 5,
		},
		repository: nil,
	})

	r.Equal(expectedRepos, actualRepos, "repositories do not match expected repositories after adding a new one")

	// Test adding a repository with equal priority
	err = fallback.AddRepositories(&resolverruntime.Resolver{
		Repository: &runtime.Raw{
			Type: runtime.NewUnversionedType("fallback55"),
		},
		Prefix:   "",
		Priority: 5,
	})
	r.NoError(err, "failed to add repository when it should succeed")

	actualRepos = fallback.fallbackRepositories
	expectedRepos = slices.Insert(expectedRepos, 1, &repositoryWithResolverRules{
		resolver: &resolverruntime.Resolver{
			Repository: &runtime.Raw{
				Type: runtime.NewUnversionedType("fallback55"),
			},
			Prefix:   "",
			Priority: 5,
		},
		repository: nil,
	})

	r.Equal(expectedRepos, actualRepos, "repositories do not match expected repositories after adding a new one")
}

func Test_ExecuteWithFallback(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	fallback := &ComponentVersionRepository{
		provider:           nil,
		credentialProvider: nil,
		fallbackRepositories: []*repositoryWithResolverRules{
			{
				resolver: &resolverruntime.Resolver{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallback2"),
					},
					Prefix:   "",
					Priority: 2,
				},
				repository: &MockComponentVersionRepository{
					typ: "fallback2",
				},
			},
			{
				resolver: &resolverruntime.Resolver{
					Repository: &runtime.Raw{
						Type: runtime.NewUnversionedType("fallback1"),
					},
					Prefix:   "",
					Priority: 1,
				},
				repository: &MockComponentVersionRepository{
					typ: "fallback1",
				},
			},
		},
	}

	usedFallbacks := make([]string, 0)
	result, err := executeWithFallback[string](ctx, fallback, "test", func(ctx context.Context, repo repository.ComponentVersionRepository) (string, error) {
		typ := repo.(*MockComponentVersionRepository).GetType()
		usedFallbacks = append(usedFallbacks, typ)
		if typ == "fallback1" {
			return "success", nil
		}
		return "failure", errdef.ErrNotFound
	})
	r.NoError(err, "operation with fallback should succeed")
	r.Equal([]string{"fallback2", "fallback1"}, usedFallbacks, "used fallbacks do not match expected order")
	r.Equal("success", result, "result should be success from fallback1")
}

func Test_ExecuteWithoutFallback(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	t.Run("without fallback and no prefixes", func(t *testing.T) {
		fallback := &ComponentVersionRepository{
			provider:           nil,
			credentialProvider: nil,
			fallbackRepositories: []*repositoryWithResolverRules{
				{
					resolver: &resolverruntime.Resolver{
						Repository: &runtime.Raw{
							Type: runtime.NewUnversionedType("fallback2"),
						},
						Prefix:   "",
						Priority: 2,
					},
					repository: &MockComponentVersionRepository{
						typ: "fallback2",
					},
				},
				{
					resolver: &resolverruntime.Resolver{
						Repository: &runtime.Raw{
							Type: runtime.NewUnversionedType("fallback1"),
						},
						Prefix:   "",
						Priority: 1,
					},
					repository: &MockComponentVersionRepository{
						typ: "fallback1",
					},
				},
			},
		}
		usedFallbacks := make([]string, 0)
		result, err := executeWithoutFallback[string](ctx, fallback, "test", func(ctx context.Context, repo repository.ComponentVersionRepository) (string, error) {
			typ := repo.(*MockComponentVersionRepository).GetType()
			usedFallbacks = append(usedFallbacks, typ)
			return "success", nil
		})
		r.NoError(err, "operation without fallback should succeed")
		r.Equal([]string{"fallback2"}, usedFallbacks, "used fallbacks do not match expected order")
		r.Equal("success", result, "result should be success from fallback2")
	})
	t.Run("without fallback and with prefixes", func(t *testing.T) {
		fallback := &ComponentVersionRepository{
			provider:           nil,
			credentialProvider: nil,
			fallbackRepositories: []*repositoryWithResolverRules{
				{
					resolver: &resolverruntime.Resolver{
						Repository: &runtime.Raw{
							Type: runtime.NewUnversionedType("fallback2"),
						},
						Prefix:   "not-matching-prefix",
						Priority: 2,
					},
					repository: &MockComponentVersionRepository{
						typ: "fallback2",
					},
				},
				{
					resolver: &resolverruntime.Resolver{
						Repository: &runtime.Raw{
							Type: runtime.NewUnversionedType("fallback1"),
						},
						Prefix:   "tes",
						Priority: 1,
					},
					repository: &MockComponentVersionRepository{
						typ: "fallback1",
					},
				},
			},
		}
		usedFallbacks := make([]string, 0)
		result, err := executeWithoutFallback[string](ctx, fallback, "test", func(ctx context.Context, repo repository.ComponentVersionRepository) (string, error) {
			typ := repo.(*MockComponentVersionRepository).GetType()
			usedFallbacks = append(usedFallbacks, typ)
			return "success", nil
		})
		r.NoError(err, "operation without fallback should succeed")
		r.Equal([]string{"fallback1"}, usedFallbacks, "used fallbacks do not match expected order")
		r.Equal("success", result, "result should be success from fallback1")
	})
}

type MockComponentVersionRepository struct {
	typ string
}

func (m *MockComponentVersionRepository) GetType() string {
	return m.typ
}

func (m *MockComponentVersionRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	//TODO implement me
	panic("implement me")
}

func (m *MockComponentVersionRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockComponentVersionRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockComponentVersionRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockComponentVersionRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockComponentVersionRepository) AddLocalSource(ctx context.Context, component, version string, res *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockComponentVersionRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	//TODO implement me
	panic("implement me")
}
