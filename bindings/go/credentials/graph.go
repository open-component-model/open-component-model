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

type Options struct {
	GetRepositoryPluginFn
	GetCredentialPluginFn
	CredentialRepositoryTypeScheme *runtime.Scheme
}

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

type Graph struct {
	repositoryConfigurationsMu sync.RWMutex
	repositoryConfigurations   []runtime.Typed

	*syncedDag

	getRepositoryPlugin GetRepositoryPluginFn
	getCredentialPlugin GetCredentialPluginFn
}

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
