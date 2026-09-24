package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/oci/internal/log"
)

// WeakEdgeFailurePolicy defines how copy traversals treat weak edges (subject
// and referrer references) whose target does not exist in the source.
// Registries accept referrers of absent subjects, and retention or mirroring
// can remove targets later. Strong edges (config, layers, index children)
// always fail the copy when their target is missing.
type WeakEdgeFailurePolicy int

const (
	// WeakEdgeFailurePolicyAbort fails the copy on a missing weak-edge target
	// (upstream ORAS behavior). This is the default.
	WeakEdgeFailurePolicyAbort WeakEdgeFailurePolicy = iota
	// WeakEdgeFailurePolicySkip logs a warning and drops the dangling edge.
	// Content reachable only through the dropped edge is not copied.
	WeakEdgeFailurePolicySkip
)

// WithWeakEdgeFailurePolicy sets the WeakEdgeFailurePolicy for the repository.
func WithWeakEdgeFailurePolicy(policy WeakEdgeFailurePolicy) RepositoryOption {
	return func(o *RepositoryOptions) {
		o.WeakEdgeFailurePolicy = policy
	}
}

// SetWeakEdgeFailurePolicy overrides the weak edge failure policy for this repository.
func (repo *Repository) SetWeakEdgeFailurePolicy(policy WeakEdgeFailurePolicy) {
	repo.weakEdgeFailurePolicy = policy
}

func (repo *Repository) copyGraphOptions() oras.CopyGraphOptions {
	opts := repo.resourceCopyOptions.CopyGraphOptions
	if repo.weakEdgeFailurePolicy == WeakEdgeFailurePolicySkip {
		opts.FindSuccessors = skipMissingSubject(opts.FindSuccessors)
	}
	return opts
}

func (repo *Repository) extendedCopyGraphOptions() oras.ExtendedCopyGraphOptions {
	opts := oras.ExtendedCopyGraphOptions{CopyGraphOptions: repo.copyGraphOptions()}
	if repo.weakEdgeFailurePolicy == WeakEdgeFailurePolicySkip {
		opts.FindPredecessors = skipMissingReferrers
	}
	return opts
}

type findSuccessorsFunc func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error)

// skipMissingSubject removes the subject from the successors of desc if the
// subject does not exist in the source.
func skipMissingSubject(next findSuccessorsFunc) findSuccessorsFunc {
	if next == nil {
		next = content.Successors
	}
	return func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
		successors, err := next(ctx, fetcher, desc)
		if err != nil {
			return nil, err
		}
		if desc.MediaType != ociImageSpecV1.MediaTypeImageManifest && desc.MediaType != ociImageSpecV1.MediaTypeImageIndex {
			return successors, nil
		}
		// The copy proxy caches the manifest from the call to next, so this fetch is local.
		raw, err := content.FetchAll(ctx, fetcher, desc)
		if err != nil {
			return nil, err
		}
		var manifest struct {
			Subject *ociImageSpecV1.Descriptor `json:"subject,omitempty"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, err
		}
		if manifest.Subject == nil {
			return successors, nil
		}
		exists, err := contentExists(ctx, fetcher, *manifest.Subject)
		if err != nil {
			return nil, fmt.Errorf("failed to check subject %s of %s: %w", manifest.Subject.Digest, desc.Digest, err)
		}
		if exists {
			return successors, nil
		}
		warnSkippedWeakEdge(ctx, "subject", *manifest.Subject)
		return slices.DeleteFunc(successors, func(s ociImageSpecV1.Descriptor) bool {
			return s.Digest == manifest.Subject.Digest
		}), nil
	}
}

// skipMissingReferrers removes listed referrers of desc whose content does not
// exist in the source.
func skipMissingReferrers(ctx context.Context, src content.ReadOnlyGraphStorage, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	predecessors, err := src.Predecessors(ctx, desc)
	if err != nil {
		return nil, err
	}
	kept := make([]ociImageSpecV1.Descriptor, 0, len(predecessors))
	for _, p := range predecessors {
		exists, err := src.Exists(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("failed to check referrer %s of %s: %w", p.Digest, desc.Digest, err)
		}
		if !exists {
			warnSkippedWeakEdge(ctx, "referrer", p)
			continue
		}
		kept = append(kept, p)
	}
	return kept, nil
}

// contentExists uses Exists if the fetcher supports it (the ORAS copy proxy
// does) and falls back to a fetch.
func contentExists(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) (bool, error) {
	if storage, ok := fetcher.(content.ReadOnlyStorage); ok {
		return storage.Exists(ctx, desc)
	}
	rc, err := fetcher.Fetch(ctx, desc)
	if errors.Is(err, errdef.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, rc.Close()
}

func warnSkippedWeakEdge(ctx context.Context, edge string, desc ociImageSpecV1.Descriptor) {
	slogcontext.FromCtx(ctx).WarnContext(ctx, "skipping missing weak edge target during OCI copy", slog.String("edge", edge), log.DescriptorLogAttr(desc))
}
