// Package lister provides functionality for listing versions of OCI components.
// It supports two main strategies for version discovery:
// 1. Referrer-based listing: Finds versions by examining referrers to a base component (requires OCI 1.1 compatible referrer subjects)
// 2. Tag-based listing: Finds versions by examining repository tags and filtering by descriptor (this is the legacy behavior in old OCM)
//
// The package supports different lookup and sorting policies to customize the version discovery process.
package lister

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"

	indexv1 "ocm.software/open-component-model/bindings/go/oci/spec/index/component/v1"
)

var (
	ErrNoSupportedVersionLister = errors.New("no supported version lister found")
	ErrSkip                     = errors.New("candidate should be skipped from listing")
)

// LookupPolicy defines the strategy used to discover component versions.
type LookupPolicy int

const (
	// LookupPolicyReferrerWithTagFallback first attempts to find versions using referrers,
	// and falls back to tag-based listing if no referrers are found or an error occurred.
	LookupPolicyReferrerWithTagFallback LookupPolicy = iota
	// LookupPolicyTagOnly only uses tag-based listing, ignoring referrers, even if they are available.
	LookupPolicyTagOnly
)

// SortPolicy defines how discovered versions should be sorted.
type SortPolicy int

const (
	// SortPolicyLooseSemverDescending sorts versions using semantic versioning rules,
	// with newer versions appearing first. Non-semver versions are filtered out.
	SortPolicyLooseSemverDescending SortPolicy = iota
)

// Options configures the version listing process.
type Options struct {
	LookupPolicy
	SortPolicy

	TagListerOptions
	ReferrerListerOptions
}

// ReferrerListerOptions configures referrer-based version listing.
type ReferrerListerOptions struct {
	// Subject is the descriptor of the component to find versions for
	Subject ociImageSpecV1.Descriptor
	// ArtifactType filters referrers by their artifact type
	ArtifactType string
	// VersionResolver converts a referrer descriptor to a version string
	VersionResolver ReferrerVersionResolver
}

// ReferrerVersionResolver converts a referrer descriptor to a version string.
// Returns ErrSkip to exclude a referrer from the results.
type ReferrerVersionResolver func(ctx context.Context, descriptor ociImageSpecV1.Descriptor) (string, error)

// TagListerOptions configures tag-based version listing.
type TagListerOptions struct {
	// Last is the last tag to start listing from (for pagination), not supported by all registries / implementations
	Last string
	// VersionResolver converts a tag to a version string
	VersionResolver TagVersionResolver
}

// TagVersionResolver converts a tag to a version string.
// Returns ErrSkip to exclude a tag from the results.
type TagVersionResolver func(ctx context.Context, tag string) (string, error)

// Lister provides functionality to list component versions using different strategies.
type Lister struct {
	referrerLister registry.ReferrerLister
	tagLister      registry.TagLister
}

// New creates a new Lister instance from a content store.
// The store must support either referrer listing or tag listing.
func New(store content.ReadOnlyStorage) (*Lister, error) {
	referrerLister, rok := store.(registry.ReferrerLister)
	tagLister, tok := store.(registry.TagLister)
	if !rok && !tok {
		return nil, fmt.Errorf("store does not support referrer lister or tag lister: %w", ErrNoSupportedVersionLister)
	}
	return &Lister{
		referrerLister: referrerLister,
		tagLister:      tagLister,
	}, nil
}

// List discovers and returns component versions based on the provided options.
// The versions are discovered using the configured lookup policy and sorted
// according to the sort policy.
func (lister *Lister) List(ctx context.Context, opts Options) ([]string, error) {
	candidates, err := lister.listUnsorted(ctx, opts)
	if err != nil {
		return nil, err
	}
	slogcontext.FromCtx(ctx).With(slog.String("realm", "oci")).Log(ctx, slog.LevelDebug, "listed version candidates", slog.Int("count", len(candidates)))
	sorted, err := lister.sort(ctx, opts, candidates)
	if err != nil {
		return nil, err
	}
	slogcontext.FromCtx(ctx).With(slog.String("realm", "oci")).Log(ctx, slog.LevelDebug, "sorted version candidates")

	return sorted, nil
}

// listUnsorted discovers component versions without applying sorting.
// The lookup strategy is determined by the LookupPolicy in opts.
func (lister *Lister) listUnsorted(ctx context.Context, opts Options) ([]string, error) {
	switch opts.LookupPolicy {
	case LookupPolicyReferrerWithTagFallback:
		tags, err := listViaReferrrers(ctx, lister.referrerLister, opts.ReferrerListerOptions)

		fallbackNeeded := len(tags) == 0 || err != nil

		if !fallbackNeeded {
			return tags, err
		}

		tags, terr := listViaTags(ctx, lister.tagLister, opts.TagListerOptions)
		if terr != nil {
			return nil, fmt.Errorf("could not list versions via referrers or tags: %w", errors.Join(err, terr))
		}

		return tags, nil
	case LookupPolicyTagOnly:
		tags, err := listViaTags(ctx, lister.tagLister, opts.TagListerOptions)
		if err != nil {
			return nil, fmt.Errorf("could not list versions via tags alone: %w", err)
		}

		return tags, nil
	default:
		return nil, fmt.Errorf("unsupported lookup policy: %q", opts.LookupPolicy)
	}
}

// sort applies the configured sort policy to the discovered versions.
func (lister *Lister) sort(_ context.Context, opts Options, candidates []string) ([]string, error) {
	switch opts.SortPolicy {
	case SortPolicyLooseSemverDescending:
		vers := make([]*semver.Version, 0, len(candidates))
		for _, candidate := range candidates {
			ver, err := semver.NewVersion(candidate)
			if err != nil {
				continue
			}
			vers = append(vers, ver)
		}
		slices.SortFunc(vers, func(a, b *semver.Version) int {
			return b.Compare(a)
		})
		vers = slices.CompactFunc(vers, func(a *semver.Version, b *semver.Version) bool {
			return a.Equal(b)
		})

		out := make([]string, 0, len(vers))
		for _, ver := range vers {
			if ver != nil {
				out = append(out, ver.Original())
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported sort policy: %q", opts.SortPolicy)
	}
}

// listViaReferrrers discovers versions by examining referrers to a base component.
// It uses the provided VersionResolver to convert referrers to version strings.
func listViaReferrrers(ctx context.Context, lister registry.ReferrerLister, opts ReferrerListerOptions) (versions []string, err error) {
	if lister == nil {
		return nil, errors.New("referrer lister is not available")
	}

	// every time we get a callback for descriptors (i.e. from a paginated list),
	// we will spawn a goroutine for each descriptor to resolve it to a version
	wg, ctx := errgroup.WithContext(ctx)
	wg.SetLimit(runtime.NumCPU())
	var mu sync.Mutex

	list := func(referrers []ociImageSpecV1.Descriptor) error {
		slogcontext.FromCtx(ctx).With(slog.String("realm", "oci")).Log(ctx, slog.LevelDebug, "listing referrers", slog.Int("count", len(referrers)))
		for _, referrer := range referrers {
			wg.Go(func() error {
				ver, err := opts.VersionResolver(ctx, referrer)
				if errors.Is(err, ErrSkip) {
					return nil
				}
				if err != nil {
					return fmt.Errorf("error resolving referrer based version: %w", err)
				}

				mu.Lock()
				defer mu.Unlock()
				versions = append(versions, ver)
				return nil
			})
		}
		return nil
	}

	if err := lister.Referrers(ctx, indexv1.Descriptor, opts.ArtifactType, list); err != nil {
		return nil, fmt.Errorf("failed to list referrers: %w", err)
	}

	if err := wg.Wait(); err != nil {
		return nil, fmt.Errorf("error while listing referrers: %w", err)
	}

	return versions, nil
}

// listViaTags discovers versions by examining repository tags.
// It uses the provided VersionResolver to convert tags to version strings.
func listViaTags(ctx context.Context, lister registry.TagLister, opts TagListerOptions) (versions []string, err error) {
	if lister == nil {
		return nil, errors.New("tag lister is not available")
	}

	wg, ctx := errgroup.WithContext(ctx)
	var mu sync.Mutex

	// every time we get a callback for tags (i.e.g from a paginated list),
	// we will spawn a goroutine for each tag to resolve it to a version
	list := func(tags []string) error {
		slogcontext.FromCtx(ctx).With(slog.String("realm", "oci")).Log(ctx, slog.LevelDebug, "listing tags", slog.Int("count", len(tags)), slog.String("tags", strings.Join(tags, ",")))
		for _, tag := range tags {
			wg.Go(func() error {
				ver, err := opts.VersionResolver(ctx, tag)
				if errors.Is(err, ErrSkip) {
					slogcontext.FromCtx(ctx).With(slog.String("realm", "oci")).Log(ctx, slog.LevelDebug, "skipping tag", slog.String("tag", tag), slog.String("error", err.Error()))
					return nil
				}
				if err != nil {
					return fmt.Errorf("error resolving tag based version: %w", err)
				}

				mu.Lock()
				defer mu.Unlock()
				versions = append(versions, ver)
				return nil
			})
		}
		return nil
	}

	if err := lister.Tags(ctx, opts.Last, list); err != nil {
		return nil, fmt.Errorf("failed to list tags: %w", err)
	}

	if err := wg.Wait(); err != nil {
		return nil, fmt.Errorf("error while listing tags: %w", err)
	}

	return versions, nil
}
