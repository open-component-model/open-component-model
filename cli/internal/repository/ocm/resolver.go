package ocm

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
	resolverv1 "ocm.software/open-component-model/cli/internal/reference/resolver/config/v1"
)

type RepositoryResolver interface {
	v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed]
}

type FallbackBasedPriorityResolver struct {
	cfg     *resolverv1.Config // Configuration for the resolver, containing aliases and resolvers
	manager *manager.PluginManager
	graph   *credentials.Graph
}

func (resolver *FallbackBasedPriorityResolver) Resolve(ctx context.Context, ref string) (v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed], map[string]string, error) {
	referenceOptions := compref.ParseOptions{}
	if resolver.cfg != nil {
		referenceOptions.Aliases = resolver.cfg.Aliases
	}
	reference, err := compref.Parse(ref, &referenceOptions)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing component reference %q failed: %w", ref, err)
	}
	repositorySpec := reference.Repository
	plugin, err := resolver.manager.ComponentVersionRepositoryRegistry.GetPlugin(ctx, repositorySpec)
	if err != nil {
		return nil, nil, fmt.Errorf("getting plugin for repository %q failed: %w", repositorySpec, err)
	}
	var creds map[string]string
	identity, err := plugin.GetIdentity(ctx, v1.GetIdentityRequest[runtime.Typed]{Typ: repositorySpec})
	if err == nil {
		if creds, err = resolver.graph.Resolve(ctx, identity); err != nil {
			return nil, nil, fmt.Errorf("getting credentials for repository %q failed: %w", repositorySpec, err)
		}
	}
	return plugin, creds, nil
}
