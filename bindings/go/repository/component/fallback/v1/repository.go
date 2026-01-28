package v1

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob"
	resolverruntime "ocm.software/open-component-model/bindings/go/configuration/ocm/v1/runtime"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const Realm = "repository/component/fallback"

// FallbackRepository implements a fallback mechanism for component version repositories.
// The configuration is static, meaning that the resolvers are provided at creation time and cannot be changed later.
// This allows for easier locking and caching of repositories.
// If a different configuration is needed, a new instance can be created
// leveraging the GetResolvers method in combination with the
// resolverruntime.Merge function.
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
type FallbackRepository struct {
	// GoRoutineLimit limits the number of active goroutines for concurrent
	// operations.
	goRoutineLimit int

	repositoryProvider  repository.ComponentVersionRepositoryProvider
	credentialsResolver credentials.Resolver

	// The resolvers slice is a list of resolvers sorted by priority (highest first).
	// The order in this list determines the order in which repositories are
	// tried during lookup operations.
	// This list is immutable after creation.
	//
	// Deprecated
	//nolint:staticcheck // SA1019: using deprecated type within deprecated code
	resolvers []*resolverruntime.Resolver

	// This cache is based on index. So, the index of the resolver in the
	// resolver slice corresponds to the index of the repository in this slice.
	repositoriesForResolverCacheMu sync.RWMutex
	repositoriesForResolverCache   []repository.ComponentVersionRepository
}

type FallbackRepositoryOption func(*FallbackRepositoryOptions)

func WithGoRoutineLimit(numGoRoutines int) FallbackRepositoryOption {
	return func(options *FallbackRepositoryOptions) {
		options.GoRoutineLimit = numGoRoutines
	}
}

type FallbackRepositoryOptions struct {
	GoRoutineLimit int
}

// NewFallbackRepository creates a new FallbackRepository instance.
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
func NewFallbackRepository(_ context.Context, repositoryProvider repository.ComponentVersionRepositoryProvider, credentialsResolver credentials.Resolver, res []*resolverruntime.Resolver, opts ...FallbackRepositoryOption) (*FallbackRepository, error) {
	options := &FallbackRepositoryOptions{}
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
	return &FallbackRepository{
		goRoutineLimit: options.GoRoutineLimit,

		repositoryProvider:  repositoryProvider,
		credentialsResolver: credentialsResolver,

		resolvers:                    resolvers,
		repositoriesForResolverCache: make([]repository.ComponentVersionRepository, len(resolvers)),
	}, nil
}

// AddComponentVersion adds a new component version to the repository specified
// by the resolver with the highest priority and matching component prefix.
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
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
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
func (f *FallbackRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)
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
// component prefix in the order of their priority (higher priorities first).
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
func (f *FallbackRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)

	var versionsMu sync.Mutex
	accumulatedVersions := make(map[string]struct{})

	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.SetLimit(f.goRoutineLimit)

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
				accumulatedVersions[version] = struct{}{}
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
// by the resolver with the highest priority and matching component prefix.
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
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
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
func (f *FallbackRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)
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
// by the resolver with the highest priority and matching component prefix.
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
func (f *FallbackRepository) AddLocalSource(ctx context.Context, component, version string, source *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)
	for repo, err := range repos {
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		return repo.AddLocalSource(ctx, component, version, source, content)
	}
	return nil, fmt.Errorf("no repository found for component %s to add local source", component)
}

// GetLocalSource iterates through all resolvers with matching component prefix in
// the order of their priority (higher priorities first) and attempts to
// retrieve the local source from each repository.
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
func (f *FallbackRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	repos := f.RepositoriesForComponentIterator(ctx, component)
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

// RepositoriesForComponentIterator returns an iterator that yields repositories for the given component.
// Compared to RepositoriesForComponent, using the iterator allows for lazy
// evaluation and can be more efficient when only a few repositories are
// needed (e.g., when leveraged by the CLI code to do a simple GetComponentVersion).
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
func (f *FallbackRepository) RepositoriesForComponentIterator(ctx context.Context, component string) iter.Seq2[repository.ComponentVersionRepository, error] {
	return func(yield func(repository.ComponentVersionRepository, error) bool) {
		for index, resolver := range f.resolvers {
			if resolver.Prefix != "" && resolver.Prefix != component && !strings.HasPrefix(component, strings.TrimSuffix(resolver.Prefix, "/")+"/") {
				continue
			}
			repo, err := f.getRepositoryFromCache(ctx, index, resolver)
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

// GetResolvers returns a copy of the resolvers used by this repository.
//
// Deprecated: FallbackRepository is an implementation for the deprecated config
// type "ocm.config.ocm.software/v1". This concept of fallback resolvers is deprecated
// and only added for backwards compatibility.
// New concepts will likely be introduced in the future (contributions welcome!).
func (f *FallbackRepository) GetResolvers() []*resolverruntime.Resolver {
	// Return a copy of the resolvers to ensure immutability
	return deepCopyResolvers(f.resolvers)
}

// Deprecated
func (f *FallbackRepository) getRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error) {
	var creds map[string]string
	consumerIdentity, err := f.repositoryProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, specification)
	if err == nil {
		if f.credentialsResolver != nil {
			if c, err := f.credentialsResolver.Resolve(ctx, consumerIdentity); err != nil {
				// does not know the credentials package - so we need to check manually
				if errors.Is(err, credentials.ErrNotFound) {
					slog.DebugContext(ctx, fmt.Sprintf("resolving credentials for repository %q failed: %s", specification, err.Error()))
				} else {
					return nil, fmt.Errorf("resolving credentials for repository %q failed: %w", specification, err)
				}
			} else {
				creds = c
			}
		}
	} else {
		slog.DebugContext(ctx, "no credentials found for repository", "realm", Realm, "repository", specification, "error", err)
	}

	repo, err := f.repositoryProvider.GetComponentVersionRepository(ctx, specification, creds)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for %q failed: %w", specification, err)
	}
	return repo, nil
}

func (f *FallbackRepository) GetComponentVersionRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error) {
	specdata, err := json.Marshal(specification)
	if err != nil {
		return nil, fmt.Errorf("marshaling repository to json failed: %w", err)
	}
	specdata, err = jsoncanonicalizer.Transform(specdata)
	if err != nil {
		return nil, fmt.Errorf("canonicalizing repository json failed: %w", err)
	}
	for _, resolver := range f.resolvers {
		data, err := json.Marshal(resolver.Repository)
		if err != nil {
			return nil, fmt.Errorf("marshaling repository to json failed: %w", err)
		}
		data, err = jsoncanonicalizer.Transform(data)
		if err != nil {
			return nil, fmt.Errorf("canonicalizing repository json failed: %w", err)
		}
		if bytes.Equal(specdata, data) {
			repo, err := f.getRepositoryFromCache(ctx, 0, resolver)
			if err != nil {
				return nil, fmt.Errorf("getting repository for resolver %v failed: %w", resolver, err)
			}
			return repo, nil
		}
	}
	return nil, fmt.Errorf("no repository found for specification %v", specification)
}

// Deprecated
//
//nolint:staticcheck // SA1019: using deprecated type within deprecated code
func (f *FallbackRepository) getRepositoryFromCache(ctx context.Context, index int, resolver *resolverruntime.Resolver) (repository.ComponentVersionRepository, error) {
	var err error

	f.repositoriesForResolverCacheMu.RLock()
	repo := f.repositoriesForResolverCache[index]
	f.repositoriesForResolverCacheMu.RUnlock()

	if repo == nil {
		repo, err = f.getRepositoryForSpecification(ctx, resolver.Repository)
		if err != nil {
			return nil, fmt.Errorf("getting repository for resolver %v failed: %w", resolver, err)
		}
		f.repositoriesForResolverCacheMu.Lock()
		f.repositoriesForResolverCache[index] = repo
		f.repositoriesForResolverCacheMu.Unlock()
	}
	return repo, nil
}

// Deprecated
//
//nolint:staticcheck // SA1019: using deprecated type within deprecated code
func deepCopyResolvers(resolvers []*resolverruntime.Resolver) []*resolverruntime.Resolver {
	if resolvers == nil {
		return nil
	}
	//nolint:staticcheck // SA1019: using deprecated type within deprecated code
	copied := make([]*resolverruntime.Resolver, len(resolvers))
	for i, resolver := range resolvers {
		copied[i] = resolver.DeepCopy()
	}
	return copied
}
