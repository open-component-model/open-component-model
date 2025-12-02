package setup

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// RepositoryOptions configures OCM repository creation.
type RepositoryOptions struct {
	Registry        repository.ComponentVersionRepositoryProvider
	CredentialGraph credentials.Resolver
	Logger          *logr.Logger
}

// NewRepository creates an OCM repository for the given repository specification.
// The repository is resolved using the given plugin manager and credential graph.
// In case the credential graph is not set, the repository is resolved without credentials.
func NewRepository(ctx context.Context, repoSpec runtime.Typed, opts RepositoryOptions) (repository.ComponentVersionRepository, error) {
	if opts.Registry == nil {
		return nil, fmt.Errorf("component version registry is required")
	}

	var creds map[string]string
	identity, err := opts.Registry.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec)
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

	repo, err := opts.Registry.GetComponentVersionRepository(ctx, repoSpec, creds)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository failed: %w", err)
	}

	return repo, nil
}
