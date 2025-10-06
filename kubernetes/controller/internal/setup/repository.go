package setup

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// RepositoryOptions configures OCM repository creation.
type RepositoryOptions struct {
	PluginManager   *manager.PluginManager
	CredentialGraph credentials.GraphResolver
	Logger          logr.Logger
}

// NewRepository creates an OCM repository for the given repository specification.
// This creates a direct repository connection without fallback support.
func NewRepository(ctx context.Context, repoSpec runtime.Typed, opts RepositoryOptions) (repository.ComponentVersionRepository, error) {
	if opts.Logger.GetSink() == nil {
		opts.Logger = logr.Discard()
	}

	if opts.PluginManager == nil {
		return nil, fmt.Errorf("plugin manager is required")
	}
	var creds map[string]string
	identity, err := opts.PluginManager.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec)
	if err == nil && opts.CredentialGraph != nil {
		creds, err = opts.CredentialGraph.Resolve(ctx, identity)
		if err != nil {
			opts.Logger.V(1).Info("failed to resolve credentials for repository",
				"repository", repoSpec,
				"error", err.Error())
		}
	} else if err != nil {
		opts.Logger.V(1).Info("could not get credential consumer identity for repository",
			"repository", repoSpec,
			"error", err.Error())
	}

	repo, err := opts.PluginManager.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, repoSpec, creds)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository failed: %w", err)
	}

	return repo, nil
}
