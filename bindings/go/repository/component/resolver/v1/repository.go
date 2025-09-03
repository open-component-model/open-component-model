package v1

import (
	"cmp"
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
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const Realm = "repository/component/resolver"

// ResolverRepository implements a resolver mechanism for component version repositories
// using regex/glob patterns for component names and semantic version constraints.
// This is the modern replacement for the deprecated fallback resolvers.
type ResolverRepository struct {
	// GoRoutineLimit limits the number of active goroutines for concurrent
	// operations.
	goRoutineLimit int

	repositoryProvider repository.ComponentVersionRepositoryProvider
	credentialProvider repository.CredentialProvider

	// The resolvers slice is a list of resolvers sorted by priority (highest first).
	// The order in this list determines the order in which repositories are
	// tried during lookup operations.
	// This list is immutable after creation.
	resolvers []*resolverruntime.Resolver

	// This cache is based on index. So, the index of the resolver in the
	// resolver slice corresponds to the index of the repository in this slice.
	repositoriesForResolverCacheMu sync.RWMutex
	repositoriesForResolverCache   []repository.ComponentVersionRepository

	// Matcher cache for resolvers
	matcherCacheMu sync.RWMutex
	matcherCache   []*matcher.ResolverMatcher
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

// NewResolverRepository creates a new ResolverRepository instance.
func NewResolverRepository(_ context.Context, repositoryProvider repository.ComponentVersionRepositoryProvider, credentialProvider repository.CredentialProvider, res []*resolverruntime.Resolver, opts ...ResolverRepositoryOption) (*ResolverRepository, error) {
	options := &ResolverRepositoryOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if options.GoRoutineLimit <= 0 {
		options.GoRoutineLimit = goruntime.NumCPU()
	}

	resolvers := deepCopyResolvers(res)
	slices.SortStableFunc(resolvers, func(a, b *resolverruntime.Resolver) int {
		return cmp.Compare(b.Priority, a.Priority)
	})

	// Pre-create matchers for all resolvers
	matchers := make([]*matcher.ResolverMatcher, len(resolvers))
	for i, resolver := range resolvers {
		matcher, err := matcher.NewResolverMatcher(resolver.ComponentName, resolver.SemVer)
		if err != nil {
			return nil, fmt.Errorf("failed to create matcher for resolver %d: %w", i, err)
		}
		matchers[i] = matcher
	}

	return &ResolverRepository{
		goRoutineLimit: options.GoRoutineLimit,

		repositoryProvider: repositoryProvider,
		credentialProvider: credentialProvider,

		resolvers:                    resolvers,
		repositoriesForResolverCache: make([]repository.ComponentVersionRepository, len(resolvers)),
		matcherCache:                 matchers,
	}, nil
}

// AddComponentVersion adds a new component version to the repository specified
// by the resolver with the highest priority and matching component name/version.
func (r *ResolverRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	repos := r.RepositoriesForComponentIterator(ctx, descriptor.Component.Name, descriptor.Component.Version)
	for repo, err := range repos {
		if err != nil {
			return fmt.Errorf("getting repository for component %s failed: %w", descriptor.Component.Name, err)
		}
		return repo.AddComponentVersion(ctx, descriptor)
	}
	return fmt.Errorf("no repository found for component %s/%s to add version", descriptor.Component.Name, descriptor.Component.Version)
}

// GetComponentVersion iterates through all resolvers with matching component name/version in
// the order of their priority (higher priorities first) and attempts to
// retrieve the component version from each repository.
func (r *ResolverRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component, version)
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

// ListComponentVersions accumulates a deduplicated list of the versions of the
// given component from all repositories specified by resolvers with a matching
// component name pattern in the order of their priority (higher priorities first).
func (r *ResolverRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component, "")

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
				// Only include versions that match the version constraint
				if r.matchesAnyResolver(component, version) {
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

// AddLocalResource adds a local resource to the repository specified
// by the resolver with the highest priority and matching component name/version.
func (r *ResolverRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component, version)
	for repo, err := range repos {
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		return repo.AddLocalResource(ctx, component, version, res, content)
	}
	return nil, fmt.Errorf("no repository found for component %s/%s to add local resource", component, version)
}

// GetLocalResource iterates through all resolvers with matching component name/version in
// the order of their priority (higher priorities first) and attempts to
// retrieve the local resource from each repository.
func (r *ResolverRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component, version)
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

// AddLocalSource adds a local source to the repository specified
// by the resolver with the highest priority and matching component name/version.
func (r *ResolverRepository) AddLocalSource(ctx context.Context, component, version string, source *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component, version)
	for repo, err := range repos {
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		return repo.AddLocalSource(ctx, component, version, source, content)
	}
	return nil, fmt.Errorf("no repository found for component %s/%s to add local source", component, version)
}

// GetLocalSource iterates through all resolvers with matching component name/version in
// the order of their priority (higher priorities first) and attempts to
// retrieve the local source from each repository.
func (r *ResolverRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	repos := r.RepositoriesForComponentIterator(ctx, component, version)
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

// RepositoriesForComponentIterator returns an iterator that yields repositories for the given component and version.
// Compared to RepositoriesForComponent, using the iterator allows for lazy
// evaluation and can be more efficient when only a few repositories are
// needed (e.g., when leveraged by the CLI code to do a simple GetComponentVersion).
func (r *ResolverRepository) RepositoriesForComponentIterator(ctx context.Context, component, version string) iter.Seq2[repository.ComponentVersionRepository, error] {
	return func(yield func(repository.ComponentVersionRepository, error) bool) {
		for index, resolver := range r.resolvers {
			// Check if this resolver matches the component name and version
			if !r.matcherCache[index].Match(component, version) {
				continue
			}

			repo, err := r.getRepositoryFromCache(ctx, index, resolver)
			if err != nil {
				yield(nil, fmt.Errorf("getting repository for resolver %v failed: %w", resolver, err))
				return
			}
			slog.DebugContext(ctx, "yielding repository for component", "realm", Realm, "component", component, "version", version, "repository", resolver.Repository)
			if !yield(repo, nil) {
				return
			}
		}
	}
}

// GetResolvers returns a copy of the resolvers used by this repository.
func (r *ResolverRepository) GetResolvers() []*resolverruntime.Resolver {
	// Return a copy of the resolvers to ensure immutability
	return deepCopyResolvers(r.resolvers)
}

// matchesAnyResolver checks if the component name and version match any resolver.
func (r *ResolverRepository) matchesAnyResolver(component, version string) bool {
	for _, matcher := range r.matcherCache {
		if matcher.Match(component, version) {
			return true
		}
	}
	return false
}

func (r *ResolverRepository) getRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error) {
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

func (r *ResolverRepository) getRepositoryFromCache(ctx context.Context, index int, resolver *resolverruntime.Resolver) (repository.ComponentVersionRepository, error) {
	var err error

	r.repositoriesForResolverCacheMu.RLock()
	repo := r.repositoriesForResolverCache[index]
	r.repositoriesForResolverCacheMu.RUnlock()

	if repo == nil {
		repo, err = r.getRepositoryForSpecification(ctx, resolver.Repository)
		if err != nil {
			return nil, fmt.Errorf("getting repository for resolver %v failed: %w", resolver, err)
		}
		r.repositoriesForResolverCacheMu.Lock()
		r.repositoriesForResolverCache[index] = repo
		r.repositoriesForResolverCacheMu.Unlock()
	}
	return repo, nil
}

func deepCopyResolvers(resolvers []*resolverruntime.Resolver) []*resolverruntime.Resolver {
	if resolvers == nil {
		return nil
	}
	copied := make([]*resolverruntime.Resolver, len(resolvers))
	for i, resolver := range resolvers {
		copied[i] = resolver.DeepCopy()
	}
	return copied
}
