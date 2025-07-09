// Package ocm provides functionality for interacting with OCM (Open Component Model) repositories.
// It offers a high-level interface for managing component versions, handling credentials,
// and performing repository operations through plugin-based implementations.
package ocm

import (
	"context"
	"fmt"
	"sync"

	"github.com/Masterminds/semver/v3"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// ComponentRepository is a wrapper around the [v1.ReadWriteOCMRepositoryPluginContract] that provides
// useful CLI relevant helper functions that make high level operations easier.
// It manages component references, repository specifications, and credentials for OCM operations.
type ComponentRepository struct {
	ref  *compref.Ref                                          // Component reference containing repository and component information
	spec runtime.Typed                                         // Repository specification
	base componentversionrepository.ComponentVersionRepository // Base repository plugin contract

	credentials map[string]string // Credentials for repository access
}

// NewFromRef creates a new ComponentRepository instance for the given component reference.
// It resolves the appropriate plugin and credentials for the repository.
func NewFromRef(ctx context.Context, manager *manager.PluginManager, graph *credentials.Graph, componentReference string) (*ComponentRepository, error) {
	ref, err := compref.Parse(componentReference)
	if err != nil {
		return nil, fmt.Errorf("parsing component reference %q failed: %w", componentReference, err)
	}

	repositorySpec := ref.Repository
	plugin, err := manager.ComponentVersionRepositoryRegistry.GetPlugin(ctx, repositorySpec)
	if err != nil {
		return nil, fmt.Errorf("getting plugin for repository %q failed: %w", repositorySpec, err)
	}

	var creds map[string]string
	identity, err := plugin.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repositorySpec)
	if err == nil {
		if creds, err = graph.Resolve(ctx, identity); err != nil {
			return nil, fmt.Errorf("getting credentials for repository %q failed: %w", repositorySpec, err)
		}
	}

	provider, err := plugin.GetComponentVersionRepository(ctx, repositorySpec, creds)
	if err != nil {
		return nil, fmt.Errorf("getting repository %q failed: %w", repositorySpec, err)
	}

	return &ComponentRepository{
		ref:         ref,
		spec:        repositorySpec,
		base:        provider,
		credentials: creds,
	}, nil
}

// ComponentReference returns the component reference associated with this repository.
func (repo *ComponentRepository) ComponentReference() *compref.Ref {
	return repo.ref
}

// GetComponentVersionsOptions configures how component versions are retrieved.
type GetComponentVersionsOptions struct {
	VersionOptions
	ConcurrencyLimit int // Maximum number of concurrent version retrievals
}

// GetComponentVersions retrieves component version descriptors based on the provided options.
// It supports concurrent retrieval of multiple versions with a configurable limit.
func (repo *ComponentRepository) GetComponentVersions(ctx context.Context, opts GetComponentVersionsOptions) ([]*descriptor.Descriptor, error) {
	versions, err := repo.Versions(ctx, opts.VersionOptions)
	if err != nil {
		return nil, fmt.Errorf("getting component versions failed: %w", err)
	}

	descs := make([]*descriptor.Descriptor, len(versions))
	var descMu sync.Mutex

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(opts.ConcurrencyLimit)
	for i, version := range versions {
		eg.Go(func() error {
			desc, err := repo.base.GetComponentVersion(ctx, repo.ref.Component, version)
			if err != nil {
				return fmt.Errorf("getting component version failed: %w", err)
			}

			descMu.Lock()
			defer descMu.Unlock()
			descs[i] = desc

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("getting component versions failed: %w", err)
	}

	return descs, nil
}

// VersionOptions configures how versions are filtered and retrieved.
type VersionOptions struct {
	SemverConstraint string // Optional semantic version constraint for filtering
	LatestOnly       bool   // If true, only return the latest version
}

// Versions retrieve available versions for the component based on the provided options.
// It supports filtering by semantic version constraints and retrieving only the latest version.
func (repo *ComponentRepository) Versions(ctx context.Context, opts VersionOptions) ([]string, error) {
	if repo.ref.Version != "" {
		return []string{repo.ref.Version}, nil
	}

	versions, err := repo.base.ListComponentVersions(ctx, repo.ref.Component)
	if err != nil {
		return nil, fmt.Errorf("listing component versions failed: %w", err)
	}

	if opts.SemverConstraint != "" {
		if versions, err = filterBySemver(versions, opts.SemverConstraint); err != nil {
			return nil, fmt.Errorf("filtering component versions failed: %w", err)
		}
	}

	if opts.LatestOnly && len(versions) > 1 {
		return versions[:1], nil
	}

	return versions, nil
}

// filterBySemver filters a list of versions based on a semantic version constraint.
// It returns only versions that satisfy the given constraint.
func filterBySemver(versions []string, constraint string) ([]string, error) {
	filteredVersions := make([]string, 0, len(versions))
	constraints, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("parsing semantic version constraint failed: %w", err)
	}
	for _, version := range versions {
		semversion, err := semver.NewVersion(version)
		if err != nil {
			continue
		}
		if !constraints.Check(semversion) {
			continue
		}
		filteredVersions = append(filteredVersions, version)
	}
	return filteredVersions, nil
}
