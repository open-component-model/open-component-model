package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"

	"ocm.software/open-component-model/bindings/go/credentials"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	pathmatcher "ocm.software/open-component-model/bindings/go/repository/component/pathmatcher/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type pathMatcherProvider struct {
	repoProvider repository.ComponentVersionRepositoryProvider
	graph        credentials.Resolver
	specProvider *pathmatcher.SpecProvider

	lock       sync.RWMutex
	repoCache  map[string]repository.ComponentVersionRepository
	validSpecs map[string]struct{}
}

var _ ComponentVersionRepositoryForComponentProvider = (*pathMatcherProvider)(nil)

// getRepository returns a cached repository for the given specification, or creates a new one.
// It handles credential resolution and caching internally.
func (p *pathMatcherProvider) getRepository(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error) {
	// Canonicalize the specification for cache key
	specdata, err := json.Marshal(specification)
	if err != nil {
		return nil, fmt.Errorf("marshaling repository to json failed: %w", err)
	}
	specdata, err = jsoncanonicalizer.Transform(specdata)
	if err != nil {
		return nil, fmt.Errorf("canonicalizing repository json failed: %w", err)
	}
	cacheKey := string(specdata)

	// Fast path: check cache with read lock
	p.lock.RLock()
	if repo, found := p.repoCache[cacheKey]; found {
		p.lock.RUnlock()
		return repo, nil
	}
	p.lock.RUnlock()

	// Resolve credentials
	var credMap map[string]string
	consumerIdentity, err := p.repoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, specification)
	if err == nil {
		if p.graph != nil {
			if credMap, err = p.graph.Resolve(ctx, consumerIdentity); err != nil {
				if errors.Is(err, credentials.ErrNotFound) {
					slog.DebugContext(ctx, fmt.Sprintf("resolving credentials for repository %q failed: %s", specification, err.Error()))
				} else {
					return nil, fmt.Errorf("resolving credentials for repository %q failed: %w", specification, err)
				}
			}
		}
	} else {
		slog.DebugContext(ctx, "could not get credential consumer identity for component version repository", "repository", specification, "error", err)
	}

	repo, err := p.repoProvider.GetComponentVersionRepository(ctx, specification, credMap)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for %q failed: %w", specification, err)
	}

	p.lock.Lock()
	p.repoCache[cacheKey] = repo
	p.lock.Unlock()

	return repo, nil
}

func (p *pathMatcherProvider) GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error) {
	repoSpec, err := p.specProvider.GetRepositorySpec(ctx, runtime.Identity{
		descruntime.IdentityAttributeName:    component,
		descruntime.IdentityAttributeVersion: version,
	})
	if err != nil {
		return nil, fmt.Errorf("getting repository spec for component %s:%s failed: %w", component, version, err)
	}

	return p.getRepository(ctx, repoSpec)
}

func (p *pathMatcherProvider) GetComponentVersionRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error) {
	// Canonicalize the specification to check if it's valid
	specdata, err := json.Marshal(specification)
	if err != nil {
		return nil, fmt.Errorf("marshaling repository to json failed: %w", err)
	}
	specdata, err = jsoncanonicalizer.Transform(specdata)
	if err != nil {
		return nil, fmt.Errorf("canonicalizing repository json failed: %w", err)
	}

	// Check if this specification is in our validSpecs map (read lock)
	p.lock.RLock()
	_, found := p.validSpecs[string(specdata)]
	p.lock.RUnlock()

	if !found {
		return nil, fmt.Errorf("repository specification not found in configured resolvers")
	}

	return p.getRepository(ctx, specification)
}
