package credentials

import (
	"context"
	"errors"
	"fmt"
	"sync"

	cfgRuntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var scheme = runtime.NewScheme()

func init() {
	v1.MustRegister(scheme)
}

// Options represents the configuration options for creating a credential graph.
type Options struct {
	RepositoryPluginProvider
	CredentialPluginProvider
	CredentialRepositoryTypeScheme *runtime.Scheme
}

// ToGraph creates a new credential graph from the provided configuration and options.
// It initializes the graph structure and ingests the configuration into the graph.
// Returns an error if the configuration cannot be properly ingested.
func ToGraph(ctx context.Context, config *cfgRuntime.Config, opts Options) (Resolver, error) {
	g := &Graph{
		syncedDag:                newSyncedDag(),
		credentialPluginProvider: opts.CredentialPluginProvider,
		repositoryPluginProvider: opts.RepositoryPluginProvider,
	}

	if err := ingest(ctx, g, config, opts.CredentialRepositoryTypeScheme); err != nil {
		return nil, err
	}

	return g, nil
}

// Graph represents a credential resolution graph that manages repository configurations
// and provides functionality to resolve credentials for given identities.
// It supports both direct credential resolution and plugin-based resolution.
type Graph struct {
	repositoryConfigurationsMu sync.RWMutex    // Mutex to protect access to repository configurations
	repositoryConfigurations   []runtime.Typed // List of repository configurations parsed

	*syncedDag // The underlying DAG structure for managing dependencies

	repositoryPluginProvider RepositoryPluginProvider // injection for resolving custom repository types
	credentialPluginProvider CredentialPluginProvider // injection for resolving custom credential types
}

// Resolve attempts to resolve credentials for the given identity.
// It first tries direct resolution through the DAG, and if that fails,
// falls back to indirect resolution through plugins.
func (g *Graph) Resolve(ctx context.Context, identity runtime.Identity) (runtime.Typed, error) {
	if _, err := identity.ParseType(); err != nil {
		err = errors.Join(ErrUnknown, err)
		return nil, fmt.Errorf("to be resolved from the credential graph, a consumer identity type is required: %w", err)
	}

	// Attempt direct resolution via the DAG.
	creds, err := g.resolveFromGraph(ctx, identity)

	// fall back to indirect resolution if we have a repository plugin provider
	// otherwise leave error as is
	if g.repositoryPluginProvider != nil && errors.Is(err, ErrNoDirectCredentials) {
		creds, err = g.resolveFromRepository(ctx, identity)
	}

	if err != nil {
		if errors.Is(err, ErrNoDirectCredentials) || errors.Is(err, ErrNoIndirectCredentials) {
			// not found err
			err = errors.Join(ErrNotFound, err)
			return nil, fmt.Errorf("failed to resolve credentials for identity %q: %w", identity.String(), err)
		}

		err = errors.Join(ErrUnknown, err)
		return nil, fmt.Errorf("failed to resolve credentials for identity %q: %w", identity.String(), err)
	}

	return creds, nil
}
