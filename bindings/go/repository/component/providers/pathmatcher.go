package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
}

var _ SpecResolvingProvider = (*pathMatcherProvider)(nil)

func (p *pathMatcherProvider) GetRepositorySpecForComponent(ctx context.Context, component, version string) (runtime.Typed, error) {
	return p.specProvider.GetRepositorySpec(ctx, runtime.Identity{
		descruntime.IdentityAttributeName:    component,
		descruntime.IdentityAttributeVersion: version,
	})
}

func (p *pathMatcherProvider) GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error) {
	repoSpec, err := p.GetRepositorySpecForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("getting repository spec for component %s:%s failed: %w", component, version, err)
	}

	var credMap map[string]string
	consumerIdentity, err := p.repoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec)
	if err == nil {
		if p.graph != nil {
			if credMap, err = p.graph.Resolve(ctx, consumerIdentity); err != nil {
				if errors.Is(err, credentials.ErrNotFound) {
					slog.DebugContext(ctx, fmt.Sprintf("resolving credentials for repository %q failed: %s", repoSpec, err.Error()))
				} else {
					return nil, fmt.Errorf("resolving credentials for repository %q failed: %w", repoSpec, err)
				}
			}
		}
	} else {
		slog.DebugContext(ctx, "could not get credential consumer identity for component version repository", "repository", repoSpec, "error", err)
	}

	repo, err := p.repoProvider.GetComponentVersionRepository(ctx, repoSpec, credMap)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for %q failed: %w", repoSpec, err)
	}

	return repo, nil
}
