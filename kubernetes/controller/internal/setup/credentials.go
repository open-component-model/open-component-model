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

	credCfg, err := credentialsConfig.LookupCredentialConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to extract credential configuration: %w", err)
	}
	if credCfg == nil {
		credCfg = &credentialsConfig.Config{}
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
		CredentialTypeSchemeProvider: newMergedCredentialTypeSchemeProvider(
			opts.PluginManager.CredentialRepositoryRegistry,
			opts.PluginManager.CredentialTypeRegistry,
		),
	}

	graph, err := credentials.ToGraph(ctx, credCfg, credOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create credential graph: %w", err)
	}

	return graph, nil
}

// mergedCredentialTypeSchemeProvider is a helper that combines the credential repository and credential type schemes into a single provider.
// This allows the credential graph to recognize both repository types and credential types during ingestion.
type mergedCredentialTypeSchemeProvider struct {
	scheme *runtime.Scheme
}

func newMergedCredentialTypeSchemeProvider(providers ...credentials.CredentialTypeSchemeProvider) *mergedCredentialTypeSchemeProvider {
	merged := runtime.NewScheme()

	for _, provider := range providers {
		merged.MustRegisterScheme(provider.GetCredentialTypeScheme())
	}

	return &mergedCredentialTypeSchemeProvider{
		scheme: merged,
	}
}

func (m *mergedCredentialTypeSchemeProvider) GetCredentialTypeScheme() *runtime.Scheme {
	return m.scheme
}
