package v1alpha1

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	goruntime "runtime"
	"sync"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/matcher"
	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const Realm = "repository/component/resolver"

// ResolverRepository implements a component version repository with a resolver
// mechanism. It uses glob patterns to match component names to
// determine which OCM repository specification to use for resolving
// component versions.
type ResolverRepository struct {
	// GoRoutineLimit limits the number of active goroutines for concurrent
	// operations.
	goRoutineLimit int

	repositoryProvider repository.ComponentVersionRepositoryProvider
	credentialProvider repository.CredentialProvider

	// A list of resolvers to use for matching components to repositories.
	// This list is immutable after creation.
	resolvers []*resolverspec.Resolver

	// This cache is based on index. So, the index of the resolver in the
	// resolver slice corresponds to the index of the repository in this slice.
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

// NewResolverRepository creates a new ResolverRepository with the given
// repository provider, credential provider, and list of resolvers.
// The repository provider is used to create repositories based on the
// repository specifications in the resolvers.
// The credential provider is used to resolve credentials for the repositories.
func NewResolverRepository(_ context.Context, repositoryProvider repository.ComponentVersionRepositoryProvider, credentialProvider repository.CredentialProvider, res []*resolverspec.Resolver, opts ...ResolverRepositoryOption) (*ResolverRepository, error) {
	options := &ResolverRepositoryOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if options.GoRoutineLimit <= 0 {
		options.GoRoutineLimit = goruntime.NumCPU()
	}

	resolvers := deepCopyResolvers(res)

	var matchers []*matcher.ResolverMatcher
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

// GetComponentVersion retrieves a component version from the first repository
// that matches the component name. If no repository matches the component name,
// or if the component version is not found in any matching repository, an error
// is returned.
func (r *ResolverRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	for index, resolver := range r.resolvers {
		if !r.matchers[index].Match(component, "") {
			continue
		}
		repo, err := r.getRepositoryForSpecification(ctx, resolver.Repository)
		if err != nil {
			return nil, fmt.Errorf("getting repository for resolver %v failed: %w", resolver, err)
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

// ListComponentVersions lists all versions for a given component from the first
// matching repositories. If no repository matches the component name, an empty list is
// returned.
// If a repository returns an error while listing versions.
func (r *ResolverRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	for index, resolver := range r.resolvers {
		if !r.matchers[index].Match(component, "") {
			continue
		}

		repo, err := r.getRepositoryForSpecification(ctx, resolver.Repository)
		if err != nil {
			return nil, fmt.Errorf("getting repository for resolver %v failed: %w", resolver, err)
		}

		slog.DebugContext(ctx, "yielding repository for component", "realm", Realm, "component", component, "repository", resolver.Repository)
		if repo == nil {
			return nil, fmt.Errorf("no repository found for resolver %v", resolver)
		}

		return repo.ListComponentVersions(ctx, component)
	}

	return nil, fmt.Errorf("no repository found for component %s", component)
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
	if err != nil {
		return nil, fmt.Errorf("getting credential consumer identity for repository %q failed: %w", specification.Name, err)
	}

	if r.credentialProvider != nil {
		if credentials, err = r.credentialProvider.Resolve(ctx, consumerIdentity); err != nil {
			return nil, fmt.Errorf("getting credentials for resource %q failed: %w", specification.Name, err)
		}
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

// AddComponentVersion adds a new component version to the first repository
// that matches the component name.
func (r *ResolverRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	for index, resolver := range r.resolvers {
		if !r.matchers[index].Match(descriptor.Component.Name, "") {
			continue
		}
		repo, err := r.getRepositoryForSpecification(ctx, resolver.Repository)
		if err != nil {
			return fmt.Errorf("getting repository for component %s failed: %w", descriptor.Component.Name, err)
		}
		return repo.AddComponentVersion(ctx, descriptor)
	}
	return fmt.Errorf("no repository found for component %s to add version", descriptor.Component.Name)
}

// AddLocalResource adds a local resource to the first repository
// that matches the component name.
func (r *ResolverRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	for index, resolver := range r.resolvers {
		if !r.matchers[index].Match(component, "") {
			continue
		}
		repo, err := r.getRepositoryForSpecification(ctx, resolver.Repository)
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		return repo.AddLocalResource(ctx, component, version, res, content)
	}
	return nil, fmt.Errorf("no repository found for component %s to add local resource", component)
}

// GetLocalResource retrieves a local resource from the first repository
// that matches the component name. If no repository matches the component name,
// or if the local resource is not found in any matching repository, an error
// is returned.
func (r *ResolverRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	for index, resolver := range r.resolvers {
		if !r.matchers[index].Match(component, "") {
			continue
		}
		repo, err := r.getRepositoryForSpecification(ctx, resolver.Repository)
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

// AddLocalSource adds a local source to the first repository
// that matches the component name.
func (r *ResolverRepository) AddLocalSource(ctx context.Context, component, version string, source *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	for index, resolver := range r.resolvers {
		if !r.matchers[index].Match(component, "") {
			continue
		}
		repo, err := r.getRepositoryForSpecification(ctx, resolver.Repository)
		if err != nil {
			return nil, fmt.Errorf("getting repository for component %s failed: %w", component, err)
		}
		return repo.AddLocalSource(ctx, component, version, source, content)
	}
	return nil, fmt.Errorf("no repository found for component %s to add local source", component)
}

// GetLocalSource retrieves a local source from the first repository
// that matches the component name. If no repository matches the component name,
// or if the local source is not found in any matching repository, an error
// is returned.
func (r *ResolverRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	for index, resolver := range r.resolvers {
		if !r.matchers[index].Match(component, "") {
			continue
		}
		repo, err := r.getRepositoryForSpecification(ctx, resolver.Repository)
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
