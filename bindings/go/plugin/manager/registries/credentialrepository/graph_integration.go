package credentialrepository

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ credentials.RepositoryPluginProvider = &RepositoryRegistry{}

func (r *RepositoryRegistry) GetRepositoryPlugin(ctx context.Context, consumer runtime.Typed) (credentials.RepositoryPlugin, error) {
	typ, ok := r.consumerTypeRegistrations[consumer.GetType()]
	if !ok {
		return nil, fmt.Errorf("no plugin registered for consumer identity type %q", consumer.GetType())
	}

	base, ok := r.internalCredentialRepositoryPlugins[typ]
	if ok {
		return base, nil
	}

	plugin, ok := r.registry[typ]
	if !ok {
		return nil, fmt.Errorf("failed to get plugin for typ %q", typ)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		return NewCredentialRepositoryPluginConverter(existingPlugin.Plugin), nil
	}

	started, err := startAndReturnPlugin(ctx, r, &plugin)
	if err != nil {
		return nil, fmt.Errorf("failed to start plugin %s: %w", plugin.ID, err)
	}

	return NewCredentialRepositoryPluginConverter(started), nil
}
