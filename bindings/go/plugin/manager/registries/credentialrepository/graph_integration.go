package credentialrepository

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	_ credentials.RepositoryPluginProvider = &RepositoryRegistry{}
	_ credentials.RepositoryPlugin         = &credentialGraphPlugin{}
)

func (r *RepositoryRegistry) GetRepositoryPlugin(ctx context.Context, consumer runtime.Typed) (credentials.RepositoryPlugin, error) {
	typ, ok := r.consumerTypeRegistrations[consumer.GetType()]
	if !ok {
		return nil, fmt.Errorf("no plugin registered for consumer identity type %q", consumer.GetType())
	}

	base, ok := r.internalCredentialRepositoryPlugins[typ]
	if ok {
		return &credentialGraphPlugin{r.internalCredentialRepositoryScheme, base}, nil
	}

	plugin, ok := r.registry[typ]
	if !ok {
		return nil, fmt.Errorf("failed to get plugin for typ %q", typ)
	}

	if existingPlugin, ok := r.constructedPlugins[plugin.ID]; ok {
		return &credentialGraphPlugin{r.internalCredentialRepositoryScheme, existingPlugin.Plugin}, nil
	}

	started, err := startAndReturnPlugin(ctx, r, &plugin)
	if err != nil {
		return nil, fmt.Errorf("failed to start plugin %s: %w", plugin.ID, err)
	}

	return &credentialGraphPlugin{r.internalCredentialRepositoryScheme, started}, nil
}

type credentialGraphPlugin struct {
	scheme *runtime.Scheme
	v1.CredentialRepositoryPluginContract[runtime.Typed]
}

func (p *credentialGraphPlugin) ConsumerIdentityForConfig(ctx context.Context, cfg runtime.Typed) (runtime.Identity, error) {
	return p.CredentialRepositoryPluginContract.ConsumerIdentityForConfig(ctx, v1.ConsumerIdentityForConfigRequest[runtime.Typed]{
		Config: cfg,
	})
}

func (p *credentialGraphPlugin) Resolve(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, credentials map[string]string) (map[string]string, error) {
	return p.CredentialRepositoryPluginContract.Resolve(ctx, v1.ResolveRequest[runtime.Typed]{
		Config:   cfg,
		Identity: identity,
	}, credentials)
}
