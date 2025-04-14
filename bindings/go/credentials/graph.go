package credentials

import (
	"context"
	"errors"
	"fmt"
	"sync"

	. "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrNoDirectCredentials is returned when a node in the graph does not have any directly
// attached credentials. There might still be credentials available through
// plugins which can be resolved at runtime.
var ErrNoDirectCredentials = errors.New("no direct credentials found in graph")

var scheme = runtime.NewScheme()

func init() {
	v1.MustRegister(scheme)
}

// Options represents the configuration options for creating a credential graph.
type Options struct {
	GetRepositoryPluginFn
	GetCredentialPluginFn
	CredentialRepositoryTypeScheme *runtime.Scheme
}

// ToGraph creates a new credential graph from the provided configuration and options.
// It initializes the graph structure and ingests the configuration into the graph.
// Returns an error if the configuration cannot be properly ingested.
func ToGraph(ctx context.Context, config *Config, opts Options) (*Graph, error) {
	g := &Graph{
		syncedDag:           newSyncedDag(),
		getCredentialPlugin: opts.GetCredentialPluginFn,
		getRepositoryPlugin: opts.GetRepositoryPluginFn,
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
	repositoryConfigurationsMu sync.RWMutex
	repositoryConfigurations   []runtime.Typed

	*syncedDag

	getRepositoryPlugin GetRepositoryPluginFn
	getCredentialPlugin GetCredentialPluginFn
}

// Resolve attempts to resolve credentials for the given identity.
// It first tries direct resolution through the DAG, and if that fails,
// falls back to indirect resolution through plugins.
func (g *Graph) Resolve(ctx context.Context, identity runtime.Identity) (map[string]string, error) {
	if _, err := identity.ParseType(); err != nil {
		return nil, fmt.Errorf("to be resolved from the credential graph, a consumer identity type is required: %w", err)
	}

	// Attempt direct resolution via the DAG.
	creds, err := g.resolveDirect(ctx, identity)

	switch {
	case errors.Is(err, ErrNoDirectCredentials):
		// fall back to indirect resolution
		return g.resolveIndirect(ctx, identity)
	case err != nil:
		return nil, err
	}

	if len(creds) > 0 {
		return creds, nil
	}
	return nil, fmt.Errorf("failed to resolve credentials for identity %v", identity)
}
