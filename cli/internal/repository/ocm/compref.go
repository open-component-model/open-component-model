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

// compRefProvider provides a [repository.ComponentVersionRepository] based on a component reference.
type compRefProvider struct {
	// ref is the parsed component reference. It contains the repository information needed to access the
	// [repository.ComponentVersionRepository].
	ref *compref.Ref
	// manager is the [manager.PluginManager] used to access the [repository.ComponentVersionRepository].
	manager *manager.PluginManager
	// graph is the [credentials.GraphResolver] used to resolve credentials for the repository.
	// It can be nil, if no credential graph is available.
	graph credentials.GraphResolver
}

// newFromCompRef creates a new compRefProvider based on the provided component reference string.
// It uses the provided PluginManager to access the [repository.ComponentVersionRepository].
func newFromCompRef(ref *compref.Ref,
	manager *manager.PluginManager,
	graph credentials.GraphResolver,
) (*compRefProvider, error) {
	return &compRefProvider{
		ref:     ref,
		manager: manager,
		graph:   graph,
	}, nil
}

// GetComponentVersionRepository returns a [repository.ComponentVersionRepository] based on the component reference.
// It resolves any necessary credentials using the credential graph if available.
// It uses the [manager.PluginManager] to access the [repository.ComponentVersionRepository].
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
	return c.manager.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, c.ref.Repository, credMap)
}
