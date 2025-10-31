package ocm

import (
	"context"
	"fmt"
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

			semvers, err := convertToSemverVersions(versions)
			if err != nil {
				return fmt.Errorf("found invalid semver version: %w", err)
			}

			// If latestOnly, find and fetch only the latest version
			if options.latestOnly {
				if len(semvers) == 0 {
					return nil
				}

				semvers = semvers[:1]
			}

			descs := make([]*descriptor.Descriptor, 0, len(semvers))
			for _, version := range semvers {
				desc, err := repo.GetComponentVersion(ctx, compName, version.Original())
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
		return sortDescriptorsBySemver(result)
	}

	return result, nil
}

// convertToSemverVersions attempts to convert all versions to semver for sorting.
func convertToSemverVersions(versions []string) ([]*semver.Version, error) {
	semvers := make([]*semver.Version, 0, len(versions))
	for _, version := range versions {
		semverVersion, err := semver.NewVersion(version)
		if err != nil {
			return nil, fmt.Errorf("parsing version %q failed: %w", version, err)
		}
		semvers = append(semvers, semverVersion)
	}

	slices.SortFunc(semvers, func(a, b *semver.Version) int {
		return b.Compare(a)
	})

	return semvers, nil
}

// versionConvertedDescriptors contains the descriptor and its version converted to semver.
type versionConvertedDescriptors struct {
	desc    *descriptor.Descriptor
	version *semver.Version
}

// sortDescriptorsBySemver sorts descriptors by semantic version in descending order (newest first).
func sortDescriptorsBySemver(descs []*descriptor.Descriptor) ([]*descriptor.Descriptor, error) {
	vcd := make([]*versionConvertedDescriptors, 0, len(descs))
	for _, d := range descs {
		sv, err := semver.NewVersion(d.Component.Version)
		if err != nil {
			return nil, fmt.Errorf("parsing version %q failed: %w", d.Component.Version, err)
		}
		vcd = append(vcd, &versionConvertedDescriptors{desc: d, version: sv})
	}

	slices.SortFunc(vcd, func(a, b *versionConvertedDescriptors) int {
		return b.version.Compare(a.version)
	})

	return slices.Collect[*descriptor.Descriptor](func(yield func(*descriptor.Descriptor) bool) {
		for _, d := range vcd {
			yield(d.desc)
		}
	}), nil
}
