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
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ComponentVersionRepositoryProvider interface {
	GetComponentVersionRepository(ctx context.Context, identity runtime.Identity) (repository.ComponentVersionRepository, error)
}

func Version(ctx context.Context, component, version string, repo repository.ComponentVersionRepository) (string, error) {
	if version == "" {
		versions, err := Versions(ctx, VersionOptions{LatestOnly: true}, component, version, repo)
		if err != nil {
			return "", fmt.Errorf("getting component versions failed: %w", err)
		}
		if len(versions) == 0 {
			return "", fmt.Errorf("no versions found for component %q", component)
		}
		if len(versions) > 1 {
			return "", fmt.Errorf("multiple versions found for component %q, expected only one: %v", component, versions)
		}
		version = versions[0]
	}
	return version, nil
}

func GetComponentVersion(ctx context.Context, component, version string, repo repository.ComponentVersionRepository) (*descriptor.Descriptor, error) {
	version, err := Version(ctx, component, version, repo)
	if err != nil {
		return nil, fmt.Errorf("getting component version failed: %w", err)
	}

	desc, err := repo.GetComponentVersion(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("getting component descriptor for %q failed: %w", component, err)
	}

	return desc, nil
}

func GetLocalResource(ctx context.Context, identity runtime.Identity, component, version string, repo repository.ComponentVersionRepository) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	version, err := Version(ctx, component, version, repo)
	if err != nil {
		return nil, nil, fmt.Errorf("getting component version failed: %w", err)
	}

	return repo.GetLocalResource(ctx, component, version, identity)
}

// GetComponentVersionsOptions configures how component versions are retrieved.
type GetComponentVersionsOptions struct {
	VersionOptions
	ConcurrencyLimit int // Maximum number of concurrent version retrievals
}

// GetComponentVersions retrieves component version descriptors based on the provided options.
// It supports concurrent retrieval of multiple versions with a configurable limit.
func GetComponentVersions(ctx context.Context, opts GetComponentVersionsOptions, component, version string, repo repository.ComponentVersionRepository) ([]*descriptor.Descriptor, error) {
	versions, err := Versions(ctx, opts.VersionOptions, component, version, repo)
	if err != nil {
		return nil, fmt.Errorf("getting component versions failed: %w", err)
	}

	descs := make([]*descriptor.Descriptor, len(versions))
	var descMu sync.Mutex

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(opts.ConcurrencyLimit)
	for i, version := range versions {
		eg.Go(func() error {
			desc, err := repo.GetComponentVersion(ctx, component, version)
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
func Versions(ctx context.Context, opts VersionOptions, component, version string, repo repository.ComponentVersionRepository) ([]string, error) {
	if version != "" {
		return []string{version}, nil
	}

	versions, err := repo.ListComponentVersions(ctx, component)
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
