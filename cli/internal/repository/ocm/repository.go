// Package ocm provides functionality for interacting with OCM (Open Component Model) repositories.
// It offers a high-level interface for managing component versions, handling credentials,
// and performing repository operations through plugin-based implementations.
package ocm

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/Masterminds/semver/v3"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

// ComponentRepositoryProvider is a function type that provides a ComponentVersionRepository
// based on the given context and identity.
// The identity can be used to select a proper repository if the provider supports multiple repositories.
// Deprecated: will be removed
type ComponentRepositoryProvider func(ctx context.Context, identity *runtime.Identity) (*ComponentRepository, error)

// ComponentRepository is a wrapper around the [v1.ReadWriteOCMRepositoryPluginContract] that provides
// useful CLI relevant helper functions that make high level operations easier.
// It manages component references, repository specifications, and credentials for OCM operations.
// Deprecated: will be removed
type ComponentRepository struct {
	// TODO: ComponentRepository could be removed? maybe? maybe not? fabian? :D
	ref  *compref.Ref                          // Component reference containing repository and component information
	spec runtime.Typed                         // Repository specification
	base repository.ComponentVersionRepository // Base repository plugin contract

	credentials map[string]string // Credentials for repository access
}

// NewFromRef creates a new ComponentRepository instance for the given component reference.
// It resolves the appropriate plugin and credentials for the repository.
func NewFromRef(ctx context.Context, manager *manager.PluginManager, graph credentials.GraphResolver, componentReference string) (*ComponentRepository, error) {
	ref, err := compref.Parse(componentReference)
	if err != nil {
		return nil, fmt.Errorf("parsing component reference %q failed: %w", componentReference, err)
	}

	var creds map[string]string
	identity, err := manager.ComponentVersionRepositoryRegistry.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, ref.Repository)
	if err == nil {
		if graph != nil {
			if creds, err = graph.Resolve(ctx, identity); err != nil {
				slog.DebugContext(ctx, fmt.Sprintf("resolving credentials for repository %q failed: %s", ref.Repository, err.Error()))
			}
		}
	} else {
		slog.WarnContext(ctx, "could not get credential consumer identity for component version repository", "repository", ref.Repository, "error", err)
	}

	prov, err := manager.ComponentVersionRepositoryRegistry.GetComponentVersionRepository(ctx, ref.Repository, creds)
	if err != nil {
		return nil, fmt.Errorf("getting component version repository for %q failed: %w", ref.Repository, err)
	}

	return &ComponentRepository{
		ref:         ref,
		spec:        ref.Repository,
		base:        prov,
		credentials: creds,
	}, nil
}

func (repo *ComponentRepository) Version(ctx context.Context) (string, error) {
	version := repo.ref.Version
	if version == "" {
		versions, err := repo.Versions(ctx, VersionOptions{LatestOnly: true})
		if err != nil {
			return "", fmt.Errorf("getting component versions failed: %w", err)
		}
		if len(versions) == 0 {
			return "", fmt.Errorf("no versions found for component %q", repo.ref.Component)
		}
		if len(versions) > 1 {
			return "", fmt.Errorf("multiple versions found for component %q, expected only one: %v", repo.ref.Component, versions)
		}
		version = versions[0]
	}
	return version, nil
}

func (repo *ComponentRepository) GetComponentVersion(ctx context.Context) (*descriptor.Descriptor, error) {
	version, err := repo.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting component version failed: %w", err)
	}

	desc, err := repo.base.GetComponentVersion(ctx, repo.ref.Component, version)
	if err != nil {
		return nil, fmt.Errorf("getting component descriptor for %q failed: %w", repo.ref.Component, err)
	}

	return desc, nil
}

func (repo *ComponentRepository) GetLocalResource(ctx context.Context, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	version, err := repo.Version(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("getting component version failed: %w", err)
	}

	return repo.base.GetLocalResource(ctx, repo.ref.Component, version, identity)
}

func (repo *ComponentRepository) ComponentVersionRepository() repository.ComponentVersionRepository {
	return repo.base
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

	// Sort semverVersions descending (newest version first).
	slices.SortFunc(descs, func(a, b *descriptor.Descriptor) int {
		semverVersionA, err := semver.NewVersion(a.Component.Version)
		if err != nil {
			slog.ErrorContext(ctx, "failed parsing version, this may result in wrong ordering", "version", a.Component.Version, "error", err)
			return 0
		}
		semverVersionB, err := semver.NewVersion(b.Component.Version)
		if err != nil {
			slog.ErrorContext(ctx, "failed parsing version, this may result in wrong ordering", "version", b.Component.Version, "error", err)
			return 0
		}
		return semverVersionB.Compare(semverVersionA)
	})

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

	// Ensure correct order.
	// We sort here, so we do not have to import semver into each repository
	// implementation.
	slices.SortFunc(versions, func(a, b string) int {
		semverA, err := semver.NewVersion(a)
		if err != nil {
			slog.ErrorContext(ctx, "failed parsing version, this may result in wrong ordering", "version", a, "error", err)
			return 0
		}
		semverB, err := semver.NewVersion(b)
		if err != nil {
			slog.ErrorContext(ctx, "failed parsing version, this may result in wrong ordering", "version", b, "error", err)
			return 0
		}
		return semverB.Compare(semverA)
	})

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
