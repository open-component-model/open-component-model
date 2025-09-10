package v1alpha1

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	goruntime "runtime"
	"slices"
	"sync"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/matcher"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const Realm = "repository/component/resolver"

type ResolverRepository struct {
	goRoutineLimit int

	repositoryProvider repository.ComponentVersionRepositoryProvider
	credentialProvider repository.CredentialProvider

	resolvers []*resolverspec.Resolver

	matchersMu sync.RWMutex
	matchers   []*matcher.ResolverMatcher
}

type ResolverRepositoryOption func(*ResolverRepositoryOptions)

func WithGoRoutineLimit(numGoRoutines int) ResolverRepositoryOption {
	return func(options *ResolverRepositoryOptions) {
		options.GoRoutineLimit = numGoRoutines
	}
}

type ResolverRepositoryOptions struct {
	GoRoutineLimit int
}

func NewResolverRepository(_ context.Context, repositoryProvider repository.ComponentVersionRepositoryProvider, credentialProvider repository.CredentialProvider, res []*resolverspec.Resolver, opts ...ResolverRepositoryOption) (*ResolverRepository, error) {
	options := &ResolverRepositoryOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if options.GoRoutineLimit <= 0 {
		options.GoRoutineLimit = goruntime.NumCPU()
	}

	resolvers := deepCopyResolvers(res)

	matchers := make([]*matcher.ResolverMatcher, 0, len(resolvers))
	var resolverErrs []error
	for i, resolver := range resolvers {
		m, err := matcher.NewResolverMatcher(resolver.ComponentName)
		if err != nil {
			resolverErrs = append(resolverErrs, fmt.Errorf("failed to create matcher for resolver %d: %w", i, err))
			continue
		}
		matchers = append(matchers, m)
	}

	if len(resolverErrs) > 0 {
		return nil, fmt.Errorf("one or more resolvers are invalid: %w", errors.Join(resolverErrs...))
	}

	return &ResolverRepository{
		goRoutineLimit: options.GoRoutineLimit,

		repositoryProvider: repositoryProvider,
		credentialProvider: credentialProvider,

		resolvers: resolvers,
		matchers:  matchers,
	}, nil
}

func (r *ResolverRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		desc, err := repo.GetComponentVersion(ctx, component, version)
		if errors.Is(err, repository.ErrNotFound) {
			slog.DebugContext(ctx, "component version not found in repository", "realm", Realm, "repository", repo, "component", component, "version", version)
			continue // try the next repository
		}
		if err != nil {
			return nil, fmt.Errorf("getting component version %s/%s from repository %v failed: %w", component, version, repo, err)
		}
		return desc, nil
	}
	return nil, fmt.Errorf("component version %s/%s not found in any repository", component, version)
}

func (r *ResolverRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component)

	var versionsMu sync.Mutex
	accumulatedVersions := make(map[string]struct{})

	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.SetLimit(r.goRoutineLimit)

	for repo, err := range repos {
		errGroup.Go(func() error {
			if err != nil {
				return fmt.Errorf("getting repository for component %s failed: %w", component, err)
			}
			var versions []string
			versions, err = repo.ListComponentVersions(ctx, component)
			if err != nil {
				return fmt.Errorf("listing component versions for %s failed: %w", component, err)
			}
			if len(versions) == 0 {
				slog.DebugContext(ctx, "no versions found for component", "component", component, "repository", repo)
				return nil
			}
			slog.DebugContext(ctx, "found versions for component", "component", component, "versions", versions, "repository", repo)
			versionsMu.Lock()
			defer versionsMu.Unlock()
			for _, version := range versions {
				if r.matchesAnyResolver(component) {
					accumulatedVersions[version] = struct{}{}
				}
			}
			return nil
		})
	}

	if err := errGroup.Wait(); err != nil {
		return nil, fmt.Errorf("listing component versions for %s failed: %w", component, err)
	}

	versionList := slices.Collect(maps.Keys(accumulatedVersions))
	slices.Sort(versionList)

	return versionList, nil
}

func (r *ResolverRepository) RepositoriesForComponentIterator(ctx context.Context, component string) iter.Seq2[repository.ComponentVersionRepository, error] {
	return func(yield func(repository.ComponentVersionRepository, error) bool) {
		r.matchersMu.RLock()
		defer r.matchersMu.RUnlock()
		for index, resolver := range r.resolvers {
			if !r.matchers[index].Match(component, "") {
				continue
			}

			repo, err := r.getRepositoryForSpecification(ctx, resolver.Repository)
			if err != nil {
				yield(nil, fmt.Errorf("getting repository for resolver %v failed: %w", resolver, err))
				return
			}

			slog.DebugContext(ctx, "yielding repository for component", "realm", Realm, "component", component, "repository", resolver.Repository)
			if !yield(repo, nil) {
				return
			}
		}
	}
}

func (r *ResolverRepository) GetResolvers() []*resolverspec.Resolver {
	return deepCopyResolvers(r.resolvers)
}

func (r *ResolverRepository) matchesAnyResolver(component string) bool {
	r.matchersMu.RLock()
	defer r.matchersMu.RUnlock()
	for _, m := range r.matchers {
		if m.Match(component, "") {
			return true
		}
	}
	return false
}

func (r *ResolverRepository) getRepositoryForSpecification(ctx context.Context, specification *runtime.Raw) (repository.ComponentVersionRepository, error) {
	var credentials map[string]string
	consumerIdentity, err := r.repositoryProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, specification)
	if err == nil {
		if r.credentialProvider != nil {
			if credentials, err = r.credentialProvider.Resolve(ctx, consumerIdentity); err != nil {
				slog.DebugContext(ctx, fmt.Sprintf("resolving credentials for repository %q failed: %s", specification, err.Error()))
			}
		}
	} else {
		slog.DebugContext(ctx, "no credentials found for repository", "realm", Realm, "repository", specification, "error", err)
	}

	repo, err := r.repositoryProvider.GetComponentVersionRepository(ctx, specification, credentials)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for %q failed: %w", specification, err)
	}
	return repo, nil
}

func deepCopyResolvers(resolvers []*resolverspec.Resolver) []*resolverspec.Resolver {
	if resolvers == nil {
		return nil
	}
	copied := make([]*resolverspec.Resolver, 0, len(resolvers))
	for _, resolver := range resolvers {
		copied = append(copied, resolver.DeepCopy())
	}
	return copied
}

func (r *ResolverRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	repos := r.RepositoriesForComponentIterator(ctx, descriptor.Component.Name)
	for repo, err := range repos {
		if err != nil {
			return fmt.Errorf("getting repository for component %s failed: %w", descriptor.Component.Name, err)
		}
		return repo.AddComponentVersion(ctx, descriptor)
	}
	return fmt.Errorf("no repository found for component %s to add version", descriptor.Component.Name)
}

func (r *ResolverRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		return repo.AddLocalResource(ctx, component, version, res, content)
	}
	return nil, fmt.Errorf("no repository found for component %s to add local resource", component)
}

func (r *ResolverRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		data, res, err := repo.GetLocalResource(ctx, component, version, identity)
		if errors.Is(err, repository.ErrNotFound) {
			slog.DebugContext(ctx, "local resource not found in repository", "realm", Realm, "repository", repo, "component", component, "version", version, "resource identity", identity)
			continue // try the next repository
		}
		if err != nil {
			return nil, nil, fmt.Errorf("getting local resource with identity %v in component version %s/%s from repository %v failed: %w", identity, component, version, repo, err)
		}
		return data, res, nil
	}
	return nil, nil, fmt.Errorf("local resource with identity %v in component version %s/%s not found in any repository", identity, component, version)
}

func (r *ResolverRepository) AddLocalSource(ctx context.Context, component, version string, source *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		return repo.AddLocalSource(ctx, component, version, source, content)
	}
	return nil, fmt.Errorf("no repository found for component %s to add local source", component)
}

func (r *ResolverRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		data, source, err := repo.GetLocalSource(ctx, component, version, identity)
		if errors.Is(err, repository.ErrNotFound) {
			slog.DebugContext(ctx, "local source not found in repository", "realm", Realm, "repository", repo, "component", component, "version", version, "resource identity", identity)
			continue // try the next repository
		}
		if err != nil {
			return nil, nil, fmt.Errorf("getting local source with identity %v in component version %s/%s from repository %v failed: %w", identity, component, version, repo, err)
		}
		return data, source, nil
	}
	return nil, nil, fmt.Errorf("local source with identity %v in component version %s/%s not found in any repository", identity, component, version)
}
