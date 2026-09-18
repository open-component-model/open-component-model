package ocm

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"golang.org/x/sync/errgroup"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	versioningspec "ocm.software/open-component-model/bindings/go/configuration/versioning/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

// RegistryFromConfig builds a versioning registry from the central OCM
// configuration. When the config carries no versioning entry, the loose-semver
// default is returned.
func RegistryFromConfig(config *genericv1.Config) (*versioning.Registry, error) {
	cfg, err := versioningspec.Lookup(config)
	if err != nil {
		return nil, fmt.Errorf("failed to look up versioning config: %w", err)
	}
	return cfg.Registry()
}

// GetComponentVersionsOptions configures how component versions are retrieved.
type GetComponentVersionsOptions struct {
	VersionOptions
	ConcurrencyLimit int // Maximum number of concurrent version retrievals
}

// GetComponentVersions retrieves component version descriptors based on the provided options.
// It supports concurrent retrieval of multiple versions with a configurable limit.
func GetComponentVersions(ctx context.Context, opts GetComponentVersionsOptions, component, version string, repo repository.ComponentVersionRepository) ([]*descriptor.Descriptor, error) {
	var (
		versions []string
		err      error
	)
	if version != "" {
		versions = append(versions, version)
	} else {
		versions, err = VersionsWithFiltering(ctx, component, repo, opts.VersionOptions)
	}
	if err != nil {
		return nil, fmt.Errorf("getting component versions failed: %w", err)
	}

	descs := make([]*descriptor.Descriptor, len(versions))
	var descMu sync.Mutex

	eg, ctx := errgroup.WithContext(ctx)
	if opts.ConcurrencyLimit == 0 {
		opts.ConcurrencyLimit = -1
	}
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

	// Sort descending (newest version first) using the configured versioning schemes.
	reg := opts.registry()
	slices.SortFunc(descs, func(a, b *descriptor.Descriptor) int {
		c, _ := reg.Compare(b.Component.Version, a.Component.Version)
		return c
	})

	return descs, nil
}

// VersionOptions configures how versions are filtered and retrieved.
type VersionOptions struct {
	SemverConstraint string // Optional semantic version constraint for filtering
	LatestOnly       bool   // If true, only return the latest version
	// Registry defines the versioning schemes used to compare, sort, and filter
	// versions. When nil, the loose-semver default is used.
	Registry *versioning.Registry
}

// registry returns the configured versioning registry, or the loose-semver
// default when none is set.
func (o VersionOptions) registry() *versioning.Registry {
	if o.Registry != nil {
		return o.Registry
	}
	return versioning.Default()
}

// VersionsWithFiltering retrieve available versions for the component based on the provided options.
// It supports filtering by semantic version constraints and retrieving only the latest version.
func VersionsWithFiltering(ctx context.Context, component string, repo repository.ComponentVersionRepository, opts VersionOptions) ([]string, error) {
	versions, err := repo.ListComponentVersions(ctx, component)
	if err != nil {
		return nil, fmt.Errorf("listing component versions failed: %w", err)
	}

	reg := opts.registry()
	if opts.SemverConstraint != "" {
		if versions, err = reg.Filter(versions, opts.SemverConstraint); err != nil {
			return nil, fmt.Errorf("filtering component versions failed: %w", err)
		}
	}

	// Ensure correct order (newest first) using the configured versioning schemes.
	if err := reg.SortDescending(versions); err != nil {
		return nil, fmt.Errorf("sorting component versions failed: %w", err)
	}

	if opts.LatestOnly && len(versions) > 1 {
		return versions[:1], nil
	}

	return versions, nil
}
