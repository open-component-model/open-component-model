package setup

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	credentialsConfig "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CredentialGraphOptions configures credential graph initialization.
type CredentialGraphOptions struct {
	PluginManager *manager.PluginManager
	Logger        *logr.Logger
}

// NewCredentialGraph creates a credential graph from the given configuration.
// The graph resolves credentials based on consumer identities using configured repositories.
func NewCredentialGraph(ctx context.Context, config *genericv1.Config, opts CredentialGraphOptions) (credentials.Resolver, error) {
	if opts.PluginManager == nil {
		return nil, fmt.Errorf("plugin manager is required for credential graph")
	}

	credCfg, err := credentialsConfig.ExtractCredentialConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to extract credential configuration: %w", err)
	}

	credOpts := credentials.Options{
		RepositoryPluginProvider: opts.PluginManager.CredentialRepositoryRegistry,
		CredentialPluginProvider: credentials.GetCredentialPluginFn(
			// TODO: Implement credential plugins when available
			func(ctx context.Context, typed runtime.Typed) (credentials.CredentialPlugin, error) {
				return nil, fmt.Errorf("no credential plugin found for type %s", typed)
			},
		),
		CredentialRepositoryTypeScheme: opts.PluginManager.CredentialRepositoryRegistry.RepositoryScheme(),
	}

	graph, err := credentials.ToGraph(ctx, credCfg, credOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create credential graph: %w", err)
	}

	return graph, nil
}
