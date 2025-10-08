package ocm

import (
	"context"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

type compRefProvider struct {
	ref                *compref.Ref
	manager            *manager.PluginManager
	repositoryProvider repository.ComponentVersionRepositoryProvider
	graph              credentials.GraphResolver
}

func newFromCompRef(componentReference string,
	manager *manager.PluginManager,
	repositoryProvider repository.ComponentVersionRepositoryProvider,
	graph credentials.GraphResolver, options ...compref.Option) (*compRefProvider, error) {
	ref, err := compref.Parse(componentReference, options...)
	if err != nil {
		return nil, fmt.Errorf("parsing component reference: %w", err)
	}

	return &compRefProvider{
		ref:                ref,
		manager:            manager,
		repositoryProvider: repositoryProvider,
		graph:              graph,
	}, nil
}

func (c compRefProvider) GetComponentVersionRepository(ctx context.Context, _ runtime.Identity) (repository.ComponentVersionRepository, error) {
	var credMap map[string]string
	consumerIdentity, err := c.manager.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, c.ref.Repository)
	if err == nil {
		if c.graph != nil {
			if credMap, err = c.graph.Resolve(ctx, consumerIdentity); err != nil {
				slog.DebugContext(ctx, fmt.Sprintf("resolving credentials for repository %q failed: %s",
					c.ref.Repository, err.Error()))
			}
		}
	} else {
		slog.WarnContext(ctx, "could not get credential consumer identity for component version repository",
			"repository", c.ref.Repository, "error", err)
	}
	return c.repositoryProvider.GetComponentVersionRepository(ctx, c.ref.Repository, credMap)
}
