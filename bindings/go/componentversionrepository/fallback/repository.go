package fallback

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
	repository "ocm.software/open-component-model/bindings/go/componentversionrepository"
	resolverruntime "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"oras.land/oras-go/v2/errdef"
)

// FallbackComponentVersionRepository implements fallback behavior through repositories.
// There is no option to add additional repositories after creation because this
// might cause issues with the cache.
type FallbackComponentVersionRepository struct {
	*componentversionrepository.RepositoryRegistry
	credentialProvider            repository.CredentialProvider
	repositoryForComponentCacheMu sync.RWMutex
	repositoryForComponentCache   map[string]*FallbackRepository
	fallbackRepositories          []*FallbackRepository
}

type FallbackRepository struct {
	resolver   *resolverruntime.Resolver
	repository repository.ComponentVersionRepository
}

// convert resolverv1 to internal type
type FallbackComponentVersionRepositoryOptions struct {
	// FallbackResolvers is a list of resolvers that can be used to resolve component references.
	FallbackResolvers []*resolverruntime.Resolver `json:"resolvers,omitempty"`
}

func New(_ context.Context, repositories []*resolverruntime.Resolver, registry *componentversionrepository.RepositoryRegistry, credentialProvider repository.CredentialProvider) (*FallbackComponentVersionRepository, error) {
	fallbackRepo := &FallbackComponentVersionRepository{
		RepositoryRegistry:          registry,
		credentialProvider:          credentialProvider,
		repositoryForComponentCache: make(map[string]*FallbackRepository),
	}
	if err := fallbackRepo.initializeFallbackRepositories(repositories); err != nil {
		return nil, fmt.Errorf("setting fallback repositories failed: %w", err)
	}
	return fallbackRepo, nil
}

func executeWithFallback[T any](ctx context.Context, fallbackRepo *FallbackComponentVersionRepository, component string, operation func(ctx context.Context, repo repository.ComponentVersionRepository) (T, error)) (T, error) {
	var zero T

	fallback, cached := fallbackRepo.getRepositoryForComponentFromCache(component)
	if cached {
		desc, err := operation(ctx, fallback.repository)
		if err != nil {
			return zero, fmt.Errorf("getting component %q from cached repository failed: %w", component, err)
		}
		return desc, nil
	}

	// try fallback resolvers
	for _, fallback := range fallbackRepo.fallbackRepositories {
		if fallback.resolver.Prefix != "" {
			if !strings.HasPrefix(component, fallback.resolver.Prefix) {
				continue
			}
		}

		var err error
		repo := fallback.repository
		if repo == nil {
			repo, err = fallbackRepo.getRepositoryForSpecification(ctx, fallback.resolver.Repository)
			if err != nil {
				return zero, fmt.Errorf("getting repository for specification %q failed: %w", fallback.resolver.Repository, err)
			}
			// cache the repository for the resolver
			fallback.repository = repo
		}

		desc, err := operation(ctx, fallback.repository)
		if err != nil && !errors.Is(err, errdef.ErrNotFound) {
			return zero, err
		}
		if err == nil {
			func() {
				fallbackRepo.repositoryForComponentCacheMu.Lock()
				defer fallbackRepo.repositoryForComponentCacheMu.Unlock()

				fallbackRepo.repositoryForComponentCache[component] = fallback
			}()
			return desc, nil
		}
	}
	return zero, fmt.Errorf("component %q not found in repositories", component)
}

func (r *FallbackComponentVersionRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	//fallback, cached := r.getRepositoryForComponentFromCache(component)
	//if cached {
	//	desc, err := fallback.repository.GetComponentVersion(ctx, component, version)
	//	if err != nil {
	//		return nil, fmt.Errorf("getting component version %q:%s from cached repository failed: %w", component, version, err)
	//	}
	//	return desc, nil
	//}
	//
	//// try fallback resolvers
	//for _, fallback := range r.fallbackRepositories {
	//	if fallback.resolver.Prefix != "" {
	//		if !strings.HasPrefix(component, fallback.resolver.Prefix) {
	//			continue
	//		}
	//	}
	//
	//	var err error
	//	repo := fallback.repository
	//	if repo == nil {
	//		repo, err = r.getRepositoryForSpecification(ctx, fallback.resolver.Repository)
	//		if err != nil {
	//			return nil, fmt.Errorf("getting repository for specification %q failed: %w", fallback.resolver.Repository, err)
	//		}
	//		// cache the repository for the resolver
	//		fallback.repository = repo
	//	}
	//
	//	desc, err := repo.GetComponentVersion(ctx, component, version)
	//	if err != nil && !errors.Is(err, errdef.ErrNotFound) {
	//		return nil, err
	//	}
	//	if err == nil {
	//		func() {
	//			r.repositoryForComponentCacheMu.Lock()
	//			defer r.repositoryForComponentCacheMu.Unlock()
	//
	//			r.repositoryForComponentCache[component] = fallback
	//		}()
	//		return desc, nil
	//	}
	//}
	//return nil, fmt.Errorf("component version %q:%s not found in repositories", component, version)
	return executeWithFallback[*descriptor.Descriptor](ctx, r, component, func(ctx context.Context, repo repository.ComponentVersionRepository) (*descriptor.Descriptor, error) {
		return repo.GetComponentVersion(ctx, component, version)
	})
}

func (r *FallbackComponentVersionRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	fallback, cached := r.getRepositoryForComponentFromCache(component)
	if cached {
		versions, err := fallback.repository.ListComponentVersions(ctx, component)
		if err != nil {
			return nil, fmt.Errorf("getting component versions for component %q from cached repository failed: %w", component, err)
		}
		return versions, nil
	}

	// try fallback resolvers
	for _, fallback := range r.fallbackRepositories {
		if fallback.resolver.Prefix != "" {
			if !strings.HasPrefix(component, fallback.resolver.Prefix) {
				continue
			}
		}
		var err error
		repo := fallback.repository
		if repo == nil {
			repo, err = r.getRepositoryForSpecification(ctx, fallback.resolver.Repository)
			if err != nil {
				return nil, fmt.Errorf("getting repository for specification %q failed: %w", fallback.resolver.Repository, err)
			}
			// cache the repository for the resolver
			fallback.repository = repo
		}

		versions, err := repo.ListComponentVersions(ctx, component)
		if err != nil && !errors.Is(err, errdef.ErrNotFound) {
			return nil, err
		}
		if err == nil {
			func() {
				r.repositoryForComponentCacheMu.Lock()
				defer r.repositoryForComponentCacheMu.Unlock()

				r.repositoryForComponentCache[component] = fallback
			}()
			return versions, nil
		}
	}
	return nil, fmt.Errorf("component %q not found in repositories", component)
}

func (r *FallbackComponentVersionRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (r *FallbackComponentVersionRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (r *FallbackComponentVersionRepository) AddLocalSource(ctx context.Context, component, version string, res *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	//TODO implement me
	panic("implement me")
}

func (r *FallbackComponentVersionRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	//TODO implement me
	panic("implement me")
}

func (r *FallbackComponentVersionRepository) initializeFallbackRepositories(resolvers []*resolverruntime.Resolver) error {
	if len(resolvers) == 0 {
		return nil
	}

	// Sort resolvers by priority
	actual := slices.Clone(resolvers)

	slices.SortStableFunc(actual, func(a, b *resolverruntime.Resolver) int {
		return cmp.Compare(b.Priority, a.Priority)
	})

	// Create fallback repositories from resolvers
	fallbackRepositories := make([]*FallbackRepository, 0, len(actual))
	for _, resolver := range actual {
		if resolver.Repository == nil {
			continue
		}

		fallbackRepositories = append(fallbackRepositories, &FallbackRepository{
			resolver: &resolverruntime.Resolver{
				Repository: resolver.Repository,
				Prefix:     resolver.Prefix,
				Priority:   resolver.Priority,
			},
			repository: nil,
		})
	}

	r.fallbackRepositories = fallbackRepositories

	return nil
}

func (r *FallbackComponentVersionRepository) getRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error) {
	provider, err := r.RepositoryRegistry.GetPlugin(ctx, specification)
	if err != nil {
		return nil, fmt.Errorf("getting plugin for specification %q failed: %w", specification, err)
	}
	consumerIdentity, err := provider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, specification)
	if err != nil {
		return nil, fmt.Errorf("getting consumer identity for repository %q failed: %w", specification, err)
	}
	var credentials map[string]string
	if r.credentialProvider != nil {
		if credentials, err = r.credentialProvider.Resolve(ctx, consumerIdentity); err != nil {
			return nil, fmt.Errorf("resolving credentials for repository %q failed: %w", specification, err)
		}
	}
	repo, err := provider.GetComponentVersionRepository(ctx, specification, credentials)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for %q failed: %w", specification, err)
	}
	return repo, nil
}

func (r *FallbackComponentVersionRepository) getRepositoryForComponentFromCache(component string) (*FallbackRepository, bool) {
	r.repositoryForComponentCacheMu.RLock()
	defer r.repositoryForComponentCacheMu.RUnlock()

	repo, ok := r.repositoryForComponentCache[component]
	if !ok {
		return nil, false
	}
	slog.Debug("using cached repository for component", "component", component)
	return repo, true
}
