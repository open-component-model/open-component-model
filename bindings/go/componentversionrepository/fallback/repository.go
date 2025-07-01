package fallback

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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
	credentialProvider               repository.CredentialProvider
	resolvedRepositoryCacheMu        sync.RWMutex
	resolvedRepositoryCache          map[string]repository.ComponentVersionRepository
	fallbackRepositorySpecifications []*resolverruntime.Resolver
}

// convert resolverv1 to internal type
type FallbackComponentVersionRepositoryOptions struct {
	// FallbackResolvers is a list of resolvers that can be used to resolve component references.
	FallbackResolvers []*resolverruntime.Resolver `json:"resolvers,omitempty"`
}

func New(_ context.Context, repositorySpecification runtime.Typed, registry *componentversionrepository.RepositoryRegistry, credentialProvider repository.CredentialProvider, options *FallbackComponentVersionRepositoryOptions) (*FallbackComponentVersionRepository, error) {
	fallbackRepo := &FallbackComponentVersionRepository{
		RepositoryRegistry:      registry,
		credentialProvider:      credentialProvider,
		resolvedRepositoryCache: make(map[string]repository.ComponentVersionRepository),
	}
	specifications, err := fallbackRepo.getFallbackRepositorySpecifications(options.FallbackResolvers)
	if err != nil {
		return nil, fmt.Errorf("getting fallback repository specifications failed: %w", err)
	}
	specifications = slices.Insert(specifications, 0, &resolverruntime.Resolver{
		Repository: repositorySpecification,
		Prefix:     "",
		Priority:   math.MaxInt,
	})
	fallbackRepo.fallbackRepositorySpecifications = specifications
	return fallbackRepo, nil
}

func (r *FallbackComponentVersionRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	//TODO implement me
	panic("implement me")
}

func (r *FallbackComponentVersionRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	// try resolve from cache
	var repo repository.ComponentVersionRepository
	func() {
		r.resolvedRepositoryCacheMu.RLock()
		defer r.resolvedRepositoryCacheMu.RUnlock()

		if cachedRepo, ok := r.resolvedRepositoryCache[component]; ok {
			slog.Debug("using cached repository for component", "component", component)
			repo = cachedRepo
		}
	}()
	if repo != nil {
		desc, err := repo.GetComponentVersion(ctx, component, version)
		if err != nil {
			return nil, fmt.Errorf("getting component version %q:%s from cached repository failed: %w", component, version, err)
		}
		return desc, nil
	}

	// try fallback resolvers
	for _, fallback := range r.fallbackRepositorySpecifications {
		if fallback.Prefix != "" {
			if !strings.HasPrefix(component, fallback.Prefix) {
				continue
			}
		}
		repo, err := r.getRepositoryForSpecification(ctx, fallback.Repository)
		if err != nil {
			return nil, fmt.Errorf("getting repository for specification %q failed: %w", fallback.Repository, err)
		}

		desc, err := repo.GetComponentVersion(ctx, component, version)
		if err != nil && !errors.Is(err, errdef.ErrNotFound) {
			return nil, err
		}
		if err == nil {
			func() {
				r.resolvedRepositoryCacheMu.Lock()
				defer r.resolvedRepositoryCacheMu.Unlock()

				r.resolvedRepositoryCache[component] = repo
			}()
			return desc, nil
		}
	}
	return nil, fmt.Errorf("component version %q:%s not found in repositories", component, version)
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

func (r *FallbackComponentVersionRepository) getFallbackRepositorySpecifications(resolvers []*resolverruntime.Resolver) ([]*resolverruntime.Resolver, error) {
	if len(resolvers) == 0 {
		return nil, nil
	}

	// Sort resolvers by priority
	actual := slices.Clone(resolvers)

	slices.SortStableFunc(actual, func(a, b *resolverruntime.Resolver) int {
		return cmp.Compare(b.Priority, a.Priority)
	})

	// Create fallback repositories from resolvers
	fallbackRepositorySpecifications := make([]*resolverruntime.Resolver, 0, len(actual))
	for _, resolver := range actual {
		if resolver.Repository == nil {
			continue
		}

		fallbackRepositorySpecifications = append(fallbackRepositorySpecifications, &resolverruntime.Resolver{
			Repository: resolver.Repository,
			Prefix:     resolver.Prefix,
			Priority:   resolver.Priority,
		})
	}

	return fallbackRepositorySpecifications, nil
}

func (r *FallbackComponentVersionRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	// try resolve from cache
	var repo repository.ComponentVersionRepository
	func() {
		r.resolvedRepositoryCacheMu.RLock()
		defer r.resolvedRepositoryCacheMu.RUnlock()

		if cachedRepo, ok := r.resolvedRepositoryCache[component]; ok {
			slog.Debug("using cached repository for component", "component", component)
			repo = cachedRepo
		}
	}()
	if repo != nil {
		versions, err := repo.ListComponentVersions(ctx, component)
		if err != nil {
			return nil, fmt.Errorf("getting component versions for component %q from cached repository failed: %w", component, err)
		}
		return versions, nil
	}

	// try fallback resolvers
	for _, fallback := range r.fallbackRepositorySpecifications {
		if fallback.Prefix != "" {
			if !strings.HasPrefix(component, fallback.Prefix) {
				continue
			}
		}
		repo, err := r.getRepositoryForSpecification(ctx, fallback.Repository)
		if err != nil {
			return nil, fmt.Errorf("getting repository for specification %q failed: %w", fallback.Repository, err)
		}

		versions, err := repo.ListComponentVersions(ctx, component)
		if err != nil && !errors.Is(err, errdef.ErrNotFound) {
			return nil, err
		}
		if err == nil {
			func() {
				r.resolvedRepositoryCacheMu.Lock()
				defer r.resolvedRepositoryCacheMu.Unlock()

				r.resolvedRepositoryCache[component] = repo
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
