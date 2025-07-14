package fallback

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
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repositories/componentrepository"
	resolverruntime "ocm.software/open-component-model/bindings/go/repositories/componentrepository/resolver/config/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// FallbackRepository implements a fallback mechanism for component version repositories.
// The configuration is static, meaning that the resolvers are provided at creation time and cannot be changed later.
// This allows for easier locking and caching of repositories.
// If a different configuration is needed, a new instance can be created
// leveraging the GetResolvers method in combination with the
// resolverruntime.Merge function.
type FallbackRepository struct {
	repositoryProvider componentrepository.ComponentVersionRepositoryProvider
	credentialProvider componentrepository.CredentialProvider

	// The resolvers slice is a list of resolvers sorted by priority (highest first).
	// The order in this list determines the order in which repositories are
	// tried during lookup operations.
	// This list is immutable after creation.
	resolvers []*resolverruntime.Resolver

	// This cache is based on index. So, the index of the resolver in the
	// resolver slice corresponds to the index of the repository in this slice.
	repositoriesForResolverCacheMu sync.Mutex
	repositoriesForResolverCache   []componentrepository.ComponentVersionRepository
}

// NewFallbackRepository creates a new FallbackRepository instance.
func NewFallbackRepository(_ context.Context, repositoryProvider componentrepository.ComponentVersionRepositoryProvider, credentialProvider componentrepository.CredentialProvider, res ...*resolverruntime.Resolver) (*FallbackRepository, error) {
	resolvers := make([]*resolverruntime.Resolver, len(res))

	// copy the resolvers to ensure immutability
	for index, resolver := range res {
		r := resolver.DeepCopy()
		resolvers[index] = r
	}
	slices.SortStableFunc(resolvers, func(a, b *resolverruntime.Resolver) int {
		return cmp.Compare(b.Priority, a.Priority)
	})
	return &FallbackRepository{
		repositoryProvider: repositoryProvider,
		credentialProvider: credentialProvider,

		resolvers:                    resolvers,
		repositoriesForResolverCache: make([]componentrepository.ComponentVersionRepository, len(resolvers)),
	}, nil
}

// AddComponentVersion adds a new component version to the repository specified
// by the resolver with the highest priority and matching component prefix.
func (f *FallbackRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	repos := f.RepositoriesForComponentIterator(ctx, descriptor.Component.Name)
	for repo, err := range repos {
		if err != nil {
			return fmt.Errorf("getting repository for component %s failed: %w", descriptor.Component.Name, err)
		}
		return repo.AddComponentVersion(ctx, descriptor)
	}
	return fmt.Errorf("no repository found for component %s to add version", descriptor.Component.Name)
}

// GetComponentVersion iterates through all resolvers with matching component prefix in
// the order of their priority (higher priorities first) and attempts to
// retrieve the component version from each repository.
func (f *FallbackRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		desc, err := repo.GetComponentVersion(ctx, component, version)
		if err != nil {
			if errors.As(err, new(*componentrepository.ErrNotFound)) {
				slog.DebugContext(ctx, "component version not found in repository", "realm", componentrepository.Realm, "repository", repo, "component", component, "version", version)
				continue // try the next repository
			}
			return nil, fmt.Errorf("getting component version %s/%s from repository %v failed: %w", component, version, repo, err)
		}
		return desc, nil
	}
	return nil, fmt.Errorf("component version %s/%s not found in any repository", component, version)
}

// ListComponentVersions accumulates a deduplicated list of the versions of the
// given component from all repositories specified by resolvers with a matching
// component prefix in the order of their priority (higher priorities first).
func (f *FallbackRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)

	var errGroup errgroup.Group
	var versionsMu sync.Mutex
	accumulatedVersions := make(map[string]struct{})

	errGroup.SetLimit(goruntime.NumCPU())

	for repo, err := range repos {
		errGroup.Go(func() error {
			if err != nil {
				return fmt.Errorf("getting repository for component %s failed: %w", component, err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// continue
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

// AddLocalResource adds a local resource to the repository specified
// by the resolver with the highest priority and matching component prefix.
func (f *FallbackRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		return repo.AddLocalResource(ctx, component, version, res, content)
	}
	return nil, fmt.Errorf("no repository found for component %s to add local resource", component)
}

// GetLocalResource iterates through all resolvers with matching component prefix in
// the order of their priority (higher priorities first) and attempts to
// retrieve the local resource from each repository.
func (f *FallbackRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		data, res, err := repo.GetLocalResource(ctx, component, version, identity)
		if err != nil {
			if errors.As(err, new(*componentrepository.ErrNotFound)) {
				slog.DebugContext(ctx, "local resource not found in repository", "realm", componentrepository.Realm, "repository", repo, "component", component, "version", version, "resource identity", identity)
				continue // try the next repository
			}
			return nil, nil, fmt.Errorf("getting local resource with identity %v in component version %s/%s from repository %v failed: %w", identity, component, version, repo, err)
		}
		return data, res, nil
	}
	return nil, nil, fmt.Errorf("local resource with identity %v in component version %s/%s not found in any repository", identity, component, version)
}

// AddLocalSource adds a local source to the repository specified
// by the resolver with the highest priority and matching component prefix.
func (f *FallbackRepository) AddLocalSource(ctx context.Context, component, version string, source *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		return repo.AddLocalSource(ctx, component, version, source, content)
	}
	return nil, fmt.Errorf("no repository found for component %s to add local resource", component)
}

// GetLocalSource iterates through all resolvers with matching component prefix in
// the order of their priority (higher priorities first) and attempts to
// retrieve the local source from each repository.
func (f *FallbackRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		data, source, err := repo.GetLocalSource(ctx, component, version, identity)
		if err != nil {
			if errors.As(err, new(*componentrepository.ErrNotFound)) {
				slog.DebugContext(ctx, "local source not found in repository", "realm", componentrepository.Realm, "repository", repo, "component", component, "version", version, "resource identity", identity)
				continue // try the next repository
			}
			return nil, nil, fmt.Errorf("getting local source with identity %v in component version %s/%s from repository %v failed: %w", identity, component, version, repo, err)
		}
		return data, source, nil
	}
	return nil, nil, fmt.Errorf("local source with identity %v in component version %s/%s not found in any repository", identity, component, version)
}

func (f *FallbackRepository) getRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (componentrepository.ComponentVersionRepository, error) {
	consumerIdentity, err := f.repositoryProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, specification)
	if err != nil {
		return nil, fmt.Errorf("getting consumer identity for repository %q failed: %w", specification, err)
	}
	var credentials map[string]string
	if f.credentialProvider != nil {
		if credentials, err = f.credentialProvider.Resolve(ctx, consumerIdentity); err != nil {
			return nil, fmt.Errorf("resolving credentials for repository %q failed: %w", specification, err)
		}
	}
	repo, err := f.repositoryProvider.GetComponentVersionRepository(ctx, specification, credentials)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for %q failed: %w", specification, err)
	}
	return repo, nil
}

func (f *FallbackRepository) getRepositoryFromCache(ctx context.Context, index int, resolver *resolverruntime.Resolver) (componentrepository.ComponentVersionRepository, error) {
	f.repositoriesForResolverCacheMu.Lock()
	defer f.repositoriesForResolverCacheMu.Unlock()

	var err error
	var repo componentrepository.ComponentVersionRepository
	if repo = f.repositoriesForResolverCache[index]; repo == nil {
		repo, err = f.getRepositoryForSpecification(ctx, resolver.Repository)
		if err != nil {
			return nil, fmt.Errorf("getting repository for resolver %v failed: %w", resolver, err)
		}
		f.repositoriesForResolverCache[index] = repo
	}
	return repo, nil
}

func (f *FallbackRepository) RepositoriesForComponent(ctx context.Context, component string) ([]componentrepository.ComponentVersionRepository, error) {
	repositories := make([]componentrepository.ComponentVersionRepository, 0, len(f.resolvers))
	for index, resolver := range f.resolvers {
		if resolver.Prefix != "" && !strings.HasPrefix(resolver.Prefix, component) {
			continue
		}
		repo, err := f.getRepositoryFromCache(ctx, index, resolver)
		if err != nil {
			return nil, fmt.Errorf("getting repository for resolver %v failed: %w", resolver, err)
		}
		repositories = append(repositories, repo)
	}
	if len(repositories) > 0 {
		return repositories, nil
	}
	return nil, fmt.Errorf("no repositories found for component %q", component)
}

// RepositoriesForComponentIterator returns an iterator that yields repositories for the given component.
// Compared to RepositoriesForComponent, using the iterator allows for lazy
// evaluation and can be more efficient when only a few repositories are
// needed (e.g., when leveraged by the CLI code to do a simple GetComponentVersion).
func (f *FallbackRepository) RepositoriesForComponentIterator(ctx context.Context, component string) iter.Seq2[componentrepository.ComponentVersionRepository, error] {
	return func(yield func(componentrepository.ComponentVersionRepository, error) bool) {
		for index, resolver := range f.resolvers {
			if resolver.Prefix != "" && !strings.HasPrefix(component, resolver.Prefix) {
				continue
			}
			repo, err := f.getRepositoryFromCache(ctx, index, resolver)
			if err != nil {
				yield(nil, fmt.Errorf("getting repository for resolver %v failed: %w", resolver, err))
				return
			}
			if repo == nil {
				yield(nil, fmt.Errorf("repository for resolver %v is nil", resolver))
				return
			}
			slog.DebugContext(ctx, "yielding repository for component", "realm", componentrepository.Realm, "component", component, "repository", resolver.Repository)
			if !yield(repo, nil) {
				return
			}
		}
	}
}

func (f *FallbackRepository) GetResolvers() []*resolverruntime.Resolver {
	// Return a copy of the resolvers to ensure immutability
	resolversCopy := make([]*resolverruntime.Resolver, len(f.resolvers))
	for i, resolver := range f.resolvers {
		resolversCopy[i] = resolver.DeepCopy()
	}
	return resolversCopy
}
