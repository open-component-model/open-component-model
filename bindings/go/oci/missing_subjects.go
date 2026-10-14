package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/oci/internal/log"
)

// copyGraphOptions returns the copy options of the repository. Copies skip
// subjects and referrers whose target does not exist in the source, with a
// warning, instead of failing. Registries accept referrers of absent subjects,
// and retention or mirroring can remove targets later. Content reachable only
// through a skipped edge is not copied. Missing config, layers or index
// children still fail the copy.
func (repo *Repository) copyGraphOptions() oras.CopyGraphOptions {
	return repo.copyGraphOptionsWith(nil)
}

func (repo *Repository) copyGraphOptionsWith(subjects *listedSubjects) oras.CopyGraphOptions {
	opts := repo.resourceCopyOptions.CopyGraphOptions
	opts.FindSuccessors = skipMissingSubject(opts.FindSuccessors, subjects)
	return opts
}

// extendedCopyGraphOptions is [Repository.copyGraphOptions] for copies that
// also walk referrers. A referrer found through the referrers listing of a
// node has that node as its subject, so the copy uses the listed node in
// place of the subject descriptor the referrer declares. Publishers write
// subjects with wrong media types (a Docker manifest list declared as
// manifest), and ORAS keys nodes by media type, digest and size: the declared
// subject would be fetched again as a different node and fail the registry's
// Content-Type check.
func (repo *Repository) extendedCopyGraphOptions() oras.ExtendedCopyGraphOptions {
	subjects := &listedSubjects{}
	return oras.ExtendedCopyGraphOptions{
		CopyGraphOptions: repo.copyGraphOptionsWith(subjects),
		FindPredecessors: subjects.skipMissingReferrers,
	}
}

// listedSubjects maps the digest of each listed referrer to the descriptor of
// the node whose referrers listing contained it.
type listedSubjects struct {
	byReferrer sync.Map // digest.Digest -> ociImageSpecV1.Descriptor
}

// subjectOf returns declared, replaced by the listed node with the same digest
// if referrer was found in a referrers listing.
func (s *listedSubjects) subjectOf(referrer, declared ociImageSpecV1.Descriptor) ociImageSpecV1.Descriptor {
	if s == nil {
		return declared
	}
	listed, ok := s.byReferrer.Load(referrer.Digest)
	if !ok || listed.(ociImageSpecV1.Descriptor).Digest != declared.Digest {
		return declared
	}
	return listed.(ociImageSpecV1.Descriptor)
}

// mediaTypeArtifactManifest is the deprecated OCI artifact manifest, which
// can also carry a subject.
const mediaTypeArtifactManifest = "application/vnd.oci.artifact.manifest.v1+json"

type findSuccessorsFunc func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error)

// skipMissingSubject removes the subject from the successors of desc if the
// subject does not exist in the source. A subject listed in subjects replaces
// the declared one.
func skipMissingSubject(next findSuccessorsFunc, subjects *listedSubjects) findSuccessorsFunc {
	if next == nil {
		next = content.Successors
	}
	return func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
		successors, err := next(ctx, fetcher, desc)
		if err != nil {
			return nil, err
		}
		switch desc.MediaType {
		case ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.MediaTypeImageIndex, mediaTypeArtifactManifest:
		default:
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
		subject := subjects.subjectOf(desc, *manifest.Subject)
		exists, err := contentExists(ctx, fetcher, subject)
		if err != nil {
			return nil, fmt.Errorf("failed to check subject %s of %s: %w", subject.Digest, desc.Digest, err)
		}
		if !exists {
			warnSkippedMissing(ctx, "subject", subject)
			return slices.DeleteFunc(successors, func(s ociImageSpecV1.Descriptor) bool {
				return s.Digest == subject.Digest
			}), nil
		}
		for i := range successors {
			if successors[i].Digest == subject.Digest {
				successors[i] = subject
			}
		}
		return successors, nil
	}
}

// skipMissingReferrers removes listed referrers of desc whose content does not
// exist in the source and records desc as the subject of the others.
func (s *listedSubjects) skipMissingReferrers(ctx context.Context, src content.ReadOnlyGraphStorage, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
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
			warnSkippedMissing(ctx, "referrer", p)
			continue
		}
		s.byReferrer.Store(p.Digest, desc)
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

func warnSkippedMissing(ctx context.Context, edge string, desc ociImageSpecV1.Descriptor) {
	slogcontext.FromCtx(ctx).WarnContext(ctx, "skipping missing subject or referrer during OCI copy", slog.String("edge", edge), log.DescriptorLogAttr(desc))
}
