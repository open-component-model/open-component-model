package fallback

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"ocm.software/open-component-model/bindings/go/blob"
	repository "ocm.software/open-component-model/bindings/go/componentversionrepository"
	resolverruntime "ocm.software/open-component-model/bindings/go/componentversionrepository/resolver/config/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"oras.land/oras-go/v2/errdef"
)

const Realm = "componentversionrepository.fallback"

// ComponentVersionRepository implements fallback behavior through repositories.
// There is no option to add additional repositories after creation because this
// might cause issues with the cache.
// If a component has been resolved from a particular repository once, all subsequent
// requests concerning this component will use the same repository. There will be
// no more fallbacks for this component.
type ComponentVersionRepository struct {
	registry           *componentversionrepository.RepositoryRegistry
	credentialProvider repository.CredentialProvider

	repositoryForComponentCacheMu sync.RWMutex
	// TODO: remove this cache as this might lead to different behavior than
	//  the legacy resolver (e.g. if you created the component version in a
	//  higher priority repository, after you fetched it from a lower)
	repositoryForComponentCache map[string]*repositoryWithResolverRules
	fallbackRepositories        []*repositoryWithResolverRules
}

var _ repository.ComponentVersionRepository = (*ComponentVersionRepository)(nil)

type repositoryWithResolverRules struct {
	mu       sync.Mutex
	resolver *resolverruntime.Resolver
	// Cached repository for the repository specification in the resolver.
	// This is lazily initialized (so, only once the resolver has to be used for
	// the first time).
	repository repository.ComponentVersionRepository
}

// New creates a new ComponentVersionRepository with the given repositories and registry.
// The highest priority repository with matching prefix will be used to perform
// add operations (add operation will not fallback).
func New(_ context.Context, repositories []*resolverruntime.Resolver, registry *componentversionrepository.RepositoryRegistry, credentialProvider repository.CredentialProvider) (*ComponentVersionRepository, error) {
	fallbackRepo := &ComponentVersionRepository{
		registry:                    registry,
		credentialProvider:          credentialProvider,
		repositoryForComponentCache: make(map[string]*repositoryWithResolverRules),
	}
	if err := fallbackRepo.initializeFallbackRepositories(repositories); err != nil {
		return nil, fmt.Errorf("setting fallback repositories failed: %w", err)
	}
	return fallbackRepo, nil
}

func executeWithoutFallback[T any](ctx context.Context, fallbackRepo *ComponentVersionRepository, component string, operation func(ctx context.Context, repo repository.ComponentVersionRepository) (T, error)) (T, error) {
	var zero T

	fallback := fallbackRepo.fallbackRepositories[0]
	fallback.mu.Lock()
	defer fallback.mu.Unlock()

	var err error
	repo := fallback.repository
	if repo == nil {
		repo, err = fallbackRepo.getRepositoryForSpecification(ctx, fallback.resolver.Repository)
		if err != nil {
			return zero, fmt.Errorf("getting repository for specification %q failed: %w", fallback.resolver.Repository, err)
		}
		fallback.repository = repo
	}

	result, err := operation(ctx, repo)
	if err != nil {
		return zero, fmt.Errorf("operation failed: %w", err)
	}
	return result, nil
}

func executeWithFallback[T any](ctx context.Context, fallbackRepo *ComponentVersionRepository, component string, operation func(ctx context.Context, repo repository.ComponentVersionRepository) (T, error)) (T, error) {
	var zero T

	fallback, cached := fallbackRepo.getRepositoryForComponentFromCache(component)
	if cached {
		result, err := operation(ctx, fallback.repository)
		if err != nil {
			return zero, fmt.Errorf("getting component %q from cached repository failed: %w", component, err)
		}
		return result, nil
	}

	for _, fallback := range fallbackRepo.fallbackRepositories {
		// func() is used to defer the unlock until the end of each loop
		// iteration
		cont, result, err := func() (bool, T, error) {
			fallback.mu.Lock()
			defer fallback.mu.Unlock()

			if fallback.resolver.Prefix != "" {
				if !strings.HasPrefix(component, fallback.resolver.Prefix) {
					return true, zero, nil
				}
			}

			var err error
			repo := fallback.repository
			if repo == nil {
				repo, err = fallbackRepo.getRepositoryForSpecification(ctx, fallback.resolver.Repository)
				if err != nil {
					return false, zero, fmt.Errorf("getting repository for specification %q failed: %w", fallback.resolver.Repository, err)
				}
				fallback.repository = repo
			}

			result, err := operation(ctx, fallback.repository)
			if err != nil && !errors.Is(err, errdef.ErrNotFound) {
				return false, zero, err
			}
			if err == nil {
				fallbackRepo.repositoryForComponentCacheMu.Lock()
				defer fallbackRepo.repositoryForComponentCacheMu.Unlock()

				slog.DebugContext(ctx, "repository used for operation", "realm", Realm, "component", component, "repository", fallback.resolver.Repository)
				fallbackRepo.repositoryForComponentCache[component] = fallback
				return false, result, nil
			}
			return true, zero, nil
		}()
		if !cont {
			return result, err
		}
	}
	return zero, fmt.Errorf("component %q not found in repositories: %w", component, errdef.ErrNotFound)
}

func (r *ComponentVersionRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	desc, err := executeWithFallback[*descriptor.Descriptor](ctx, r, component, func(ctx context.Context, repo repository.ComponentVersionRepository) (*descriptor.Descriptor, error) {
		return repo.GetComponentVersion(ctx, component, version)
	})
	if err != nil {
		return nil, fmt.Errorf("getting component version %q:%s failed: %w", component, version, err)
	}
	return desc, nil
}

func (r *ComponentVersionRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	var errGroup errgroup.Group
	var versionsMu sync.Mutex
	accumulatedVersions := make(map[string]struct{})

	errGroup.SetLimit(goruntime.NumCPU())
	for _, fallback := range r.fallbackRepositories {
		errGroup.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// continue
			}

			fallback.mu.Lock()
			defer fallback.mu.Unlock()

			if fallback.resolver.Prefix != "" {
				if !strings.HasPrefix(component, fallback.resolver.Prefix) {
					return nil
				}
			}

			var err error
			repo := fallback.repository
			if repo == nil {
				repo, err = r.getRepositoryForSpecification(ctx, fallback.resolver.Repository)
				if err != nil {
					return fmt.Errorf("getting repository for specification %q failed: %w", fallback.resolver.Repository, err)
				}
				fallback.repository = repo
			}

			versions, err := repo.ListComponentVersions(ctx, component)
			if err != nil {
				return fmt.Errorf("listing component versions for %q failed: %w", component, err)
			}
			if len(versions) == 0 {
				slog.DebugContext(ctx, "no versions found for component", "component", component, "repository", fallback.resolver.Repository)
				return nil
			}
			slog.DebugContext(ctx, "found versions for component", "component", component, "versions", versions, "repository", fallback.resolver.Repository)
			versionsMu.Lock()
			defer versionsMu.Unlock()
			for _, version := range versions {
				accumulatedVersions[version] = struct{}{}
			}
			return nil
		})
	}

	if err := errGroup.Wait(); err != nil {
		return nil, fmt.Errorf("listing component versions for %q failed: %w", component, err)
	}

	versionList := slices.Collect(maps.Keys(accumulatedVersions))
	slices.Sort(versionList)

	return versionList, nil
}

func (r *ComponentVersionRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	if _, err := executeWithoutFallback[any](ctx, r, descriptor.Component.Name, func(ctx context.Context, repo repository.ComponentVersionRepository) (any, error) {
		return nil, repo.AddComponentVersion(ctx, descriptor)
	}); err != nil {
		return fmt.Errorf("adding component version %q:%s failed: %w", descriptor.Component.Name, descriptor.Component.Version, err)
	}
	return nil
}

func (r *ComponentVersionRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return executeWithoutFallback[*descriptor.Resource](ctx, r, component, func(ctx context.Context, repo repository.ComponentVersionRepository) (*descriptor.Resource, error) {
		resource, err := repo.AddLocalResource(ctx, component, version, res, content)
		if err != nil {
			return nil, fmt.Errorf("adding local resource %v to component %q:%s failed: %w", res.ToIdentity(), component, version, err)
		}
		return resource, nil
	})
}

func (r *ComponentVersionRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	type blobReadOnlyBlobAndResource struct {
		blob blob.ReadOnlyBlob
		res  *descriptor.Resource
	}
	result, err := executeWithFallback[blobReadOnlyBlobAndResource](ctx, r, component, func(ctx context.Context, repo repository.ComponentVersionRepository) (blobReadOnlyBlobAndResource, error) {
		roblob, resource, err := repo.GetLocalResource(ctx, component, version, identity)
		return blobReadOnlyBlobAndResource{
			blob: roblob,
			res:  resource,
		}, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("getting local resource %v for component version %q:%s failed: %w", identity, component, version, err)
	}
	return result.blob, result.res, nil
}

func (r *ComponentVersionRepository) AddLocalSource(ctx context.Context, component, version string, res *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return executeWithoutFallback[*descriptor.Source](ctx, r, component, func(ctx context.Context, repo repository.ComponentVersionRepository) (*descriptor.Source, error) {
		source, err := repo.AddLocalSource(ctx, component, version, res, content)
		if err != nil {
			return nil, fmt.Errorf("adding local source %v to component %q:%s failed: %w", res.ToIdentity(), component, version, err)
		}
		return source, nil
	})
}

func (r *ComponentVersionRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	type blobReadOnlyBlobAndSource struct {
		blob blob.ReadOnlyBlob
		res  *descriptor.Source
	}
	result, err := executeWithFallback[blobReadOnlyBlobAndSource](ctx, r, component, func(ctx context.Context, repo repository.ComponentVersionRepository) (blobReadOnlyBlobAndSource, error) {
		roblob, source, err := repo.GetLocalSource(ctx, component, version, identity)
		return blobReadOnlyBlobAndSource{
			blob: roblob,
			res:  source,
		}, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("getting local source %v for component version %q:%s failed: %w", identity, component, version, err)
	}
	return result.blob, result.res, nil
}

func (r *ComponentVersionRepository) initializeFallbackRepositories(resolvers []*resolverruntime.Resolver) error {
	if len(resolvers) == 0 {
		return nil
	}

	actual := slices.Clone(resolvers)

	slices.SortStableFunc(actual, func(a, b *resolverruntime.Resolver) int {
		return cmp.Compare(b.Priority, a.Priority)
	})

	// Create fallback repositories from resolvers
	fallbackRepositories := make([]*repositoryWithResolverRules, 0, len(actual))
	for _, resolver := range actual {
		if resolver.Repository == nil {
			continue
		}

		fallbackRepositories = append(fallbackRepositories, &repositoryWithResolverRules{
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

func (r *ComponentVersionRepository) getRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error) {
	provider, err := r.registry.GetPlugin(ctx, specification)
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

func (r *ComponentVersionRepository) getRepositoryForComponentFromCache(component string) (*repositoryWithResolverRules, bool) {
	r.repositoryForComponentCacheMu.RLock()
	defer r.repositoryForComponentCacheMu.RUnlock()

	repo, ok := r.repositoryForComponentCache[component]
	if !ok {
		return nil, false
	}
	slog.Debug("using cached repository for component", "component", component)
	return repo, true
}
