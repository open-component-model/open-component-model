package ocm

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"golang.org/x/sync/errgroup"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

// ComponentVersionsFilterOptions holds the configuration for filtering component versions.
type ComponentVersionsFilterOptions struct {
	components       []string
	semverConstraint string
	latestOnly       bool
	concurrencyLimit int
	sort             bool
	registry         *versioning.Registry
}

// registry returns the configured versioning registry, or the loose-semver
// default when none is set.
func (o *ComponentVersionsFilterOptions) reg() *versioning.Registry {
	if o.registry != nil {
		return o.registry
	}
	return versioning.Default()
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

// WithRegistry sets the versioning schemes used to filter and sort versions.
func WithRegistry(registry *versioning.Registry) ComponentVersionsFilterOption {
	return func(o *ComponentVersionsFilterOptions) {
		o.registry = registry
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

			reg := options.reg()
			if options.semverConstraint != "" {
				versions, err = reg.Filter(versions, options.semverConstraint)
				if err != nil {
					return fmt.Errorf("filtering component versions failed: %w", err)
				}
			}

			reg.SortDescending(versions)

			// If latestOnly, fetch only the newest version.
			if options.latestOnly {
				if len(versions) == 0 {
					return nil
				}
				versions = versions[:1]
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
		reg := options.reg()
		slices.SortFunc(result, func(a, b *descriptor.Descriptor) int {
			c, _ := reg.Compare(b.Component.Version, a.Component.Version)
			return c
		})
	}

	return result, nil
}
