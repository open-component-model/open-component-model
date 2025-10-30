package ocm

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/Masterminds/semver/v3"
	"golang.org/x/sync/errgroup"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
)

// ComponentVersionsFilterOptions holds the configuration for filtering component versions.
type ComponentVersionsFilterOptions struct {
	components       []string
	semverConstraint string
	latestOnly       bool
	concurrencyLimit int
	sort             bool
}

// ComponentVersionsFilterOption is a function that configures ComponentVersionsFilterOptions.
type ComponentVersionsFilterOption func(*ComponentVersionsFilterOptions)

// WithComponentNames sets the component names to retrieve versions for.
func WithComponentNames(components []string) ComponentVersionsFilterOption {
	return func(o *ComponentVersionsFilterOptions) {
		o.components = components
	}
}

// WithSemverConstraint sets the semantic version constraint for filtering.
func WithSemverConstraint(constraint string) ComponentVersionsFilterOption {
	return func(o *ComponentVersionsFilterOptions) {
		o.semverConstraint = constraint
	}
}

// WithLatestOnly configures whether to return only the latest version.
func WithLatestOnly(latestOnly bool) ComponentVersionsFilterOption {
	return func(o *ComponentVersionsFilterOptions) {
		o.latestOnly = latestOnly
	}
}

// WithConcurrencyLimit sets the maximum number of concurrent operations.
func WithConcurrencyLimit(limit int) ComponentVersionsFilterOption {
	return func(o *ComponentVersionsFilterOptions) {
		o.concurrencyLimit = limit
	}
}

// WithSort configures whether to sort descriptors by semantic version (descending).
func WithSort() ComponentVersionsFilterOption {
	return func(o *ComponentVersionsFilterOptions) {
		o.sort = true
	}
}

// ListComponentVersions retrieves component version descriptors for multiple components.
// It supports filtering by semantic version constraints and retrieving only the latest version.
func ListComponentVersions(ctx context.Context, repo repository.ComponentVersionRepository, opts ...ComponentVersionsFilterOption) ([]*descriptor.Descriptor, error) {
	options := &ComponentVersionsFilterOptions{
		concurrencyLimit: -1,
		sort:             true,
		latestOnly:       false,
	}
	for _, opt := range opts {
		opt(options)
	}

	if len(options.components) == 0 {
		return nil, nil
	}

	var result []*descriptor.Descriptor
	var mu sync.Mutex

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(options.concurrencyLimit)

	for _, compName := range options.components {
		eg.Go(func() error {
			versions, err := repo.ListComponentVersions(ctx, compName)
			if err != nil {
				return fmt.Errorf("listing component versions failed: %w", err)
			}

			if options.semverConstraint != "" {
				versions, err = filterBySemver(versions, options.semverConstraint)
				if err != nil {
					return fmt.Errorf("filtering component versions failed: %w", err)
				}
			}

			// If latestOnly, find and fetch only the latest version
			if options.latestOnly {
				versions = []string{findLatestVersion(ctx, versions)}
			}

			descs := make([]*descriptor.Descriptor, 0, len(versions))
			for _, version := range versions {
				desc, err := repo.GetComponentVersion(ctx, compName, version)
				if err != nil {
					return fmt.Errorf("getting component version failed: %w", err)
				}
				descs = append(descs, desc)
			}

			mu.Lock()
			result = append(result, descs...)
			mu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// Ensure deterministic global ordering across components and versions.
	if options.sort {
		sortDescriptorsBySemver(ctx, result)
	}

	return result, nil
}

// findLatestVersion returns the latest semantic version from a list of version strings.
// Returns empty string if no valid versions found.
func findLatestVersion(ctx context.Context, versions []string) string {
	if len(versions) == 0 {
		return ""
	}

	sortSemverVersions(ctx, versions)
	return versions[0]
}

// sortSemverVersions sorts version strings by semantic version in descending order (newest first).
func sortSemverVersions(ctx context.Context, versions []string) {
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
}

// sortDescriptorsBySemver sorts descriptors by semantic version in descending order (newest first).
func sortDescriptorsBySemver(ctx context.Context, descs []*descriptor.Descriptor) {
	slices.SortFunc(descs, func(a, b *descriptor.Descriptor) int {
		semverA, err := semver.NewVersion(a.Component.Version)
		if err != nil {
			slog.ErrorContext(ctx, "failed parsing version, this may result in wrong ordering", "version", a.Component.Version, "error", err)
			return 0
		}
		semverB, err := semver.NewVersion(b.Component.Version)
		if err != nil {
			slog.ErrorContext(ctx, "failed parsing version, this may result in wrong ordering", "version", b.Component.Version, "error", err)
			return 0
		}
		return semverB.Compare(semverA)
	})
}
