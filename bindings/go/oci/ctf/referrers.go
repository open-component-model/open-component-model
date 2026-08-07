package ctf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"

	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
)

var _ registry.ReferrerLister = (*repository)(nil)

// buildReferrersTag builds the referrers tag for the given manifest descriptor.
// Format: <algorithm>-<digest>
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#unavailable-referrers-api
//
// Copied from oras remote.buildReferrersTag.
func buildReferrersTag(desc ociImageSpecV1.Descriptor) (string, error) {
	if err := desc.Digest.Validate(); err != nil {
		return "", fmt.Errorf("failed to build referrers tag for %s: %w", desc.Digest, err)
	}
	alg := desc.Digest.Algorithm().String()
	encoded := desc.Digest.Encoded()
	return alg + "-" + encoded, nil
}

// Referrers lists the descriptors of image or artifact manifests directly
// referencing the given manifest descriptor.
//
// If artifactType is not empty, only referrers of the same artifact type are
// fed to fn.
//
// CTF does not support Referrers API. This implementation hard codes the fallback
// behavior to referrers tag schema.
//
// Reference: https://github.com/opencontainers/distribution-spec/blob/main/spec.md#referrers-tag-schema
//
// Inspired by oras remote.Repository.Referrers.
func (s *repository) Referrers(ctx context.Context, desc ociImageSpecV1.Descriptor, artifactType string, fn func(referrers []ociImageSpecV1.Descriptor) error) error {
	referrersTag, err := buildReferrersTag(desc)
	if err != nil {
		return err
	}

	s.mu.RLock()
	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		s.mu.RUnlock()
		return fmt.Errorf("unable to get index: %w", err)
	}
	_, referrers, err := s.referrersFromArtifactIndex(ctx, idx, referrersTag)
	s.mu.RUnlock()
	if err != nil {
		return err
	}

	filtered := filterReferrers(referrers, artifactType)
	if len(filtered) == 0 {
		return nil
	}
	return fn(filtered)
}

// Predecessors returns the descriptors of image or artifact manifests directly
// referencing the given manifest descriptor.
// Predecessors internally leverages Referrers.
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#listing-referrers
//
// Copied from oras remote.Repository.Predecessors.
func (s *repository) Predecessors(ctx context.Context, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	var res []ociImageSpecV1.Descriptor
	if err := s.Referrers(ctx, desc, "", func(referrers []ociImageSpecV1.Descriptor) error {
		res = append(res, referrers...)
		return nil
	}); err != nil {
		return nil, err
	}
	return res, nil
}

// referrersFromIndex queries the referrers index using the given referrers
// tag. On success, returns the descriptor of the referrers index manifest
// (zero value if no index exists) and the referrers list it contains.
//
// The caller MUST hold a read or a write lock.
func (s *repository) referrersFromArtifactIndex(ctx context.Context, idx v1.Index, referrersTag string) (indexDesc ociImageSpecV1.Descriptor, referrers []ociImageSpecV1.Descriptor, err error) {
	desc, rc, err := s.fetchReference(ctx, idx, referrersTag)
	if errors.Is(err, errdef.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		// valid case: no referrers index for this subject, or the index
		// entry is dangling. Self-heal on the next push.
		return ociImageSpecV1.Descriptor{}, nil, nil
	}
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("unable to fetch referrers index for referrers tag %q: %w", referrersTag, err)
	}
	defer func() {
		err = errors.Join(err, rc.Close())
	}()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("unable to read referrers index %q for referrers tag %q: %w", desc.Digest, referrersTag, err)
	}

	var refIdx ociImageSpecV1.Index
	if err := json.Unmarshal(raw, &refIdx); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to decode referrers index from referrers tag %q: %w", referrersTag, err)
	}
	return desc, refIdx.Manifests, nil
}

// updateReferrersIndex updates the referrers index for desc referencing subject
// on manifest push.
// References:
//   - https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#pushing-manifests-with-subject
//
// CTF does not implement content.Deleter and does not implement the
// SkipReferrerGC negotiation; this is the inline equivalent. Each push that
// changes the index writes a new index blob, retags the referrers tag onto
// it, and best-effort deletes the prior index blob. Failures during cleanup
// are logged but do not fail the push: stale blobs are harmless dead weight,
// not correctness bugs. The new index is intentionally not tagged by digest —
// referrers indexes are bookkeeping, never resolved by digest, and a digest
// tag would leave a dangling index entry behind on the next push.
//
// The caller MUST hold a write lock and MUST persist the index after the call.
func (s *repository) updateReferrersIndex(ctx context.Context, idx v1.Index, subject, referrer ociImageSpecV1.Descriptor) error {
	referrersTag, err := buildReferrersTag(subject)
	if err != nil {
		return err
	}

	oldIndexDesc, oldReferrers, err := s.referrersFromArtifactIndex(ctx, idx, referrersTag)
	if err != nil {
		return err
	}

	updated, changed := addReferrer(oldReferrers, referrer)
	if !changed {
		// the referrer is already indexed and the stored index is clean;
		// skip the write entirely, making referrer re-pushes idempotent.
		return nil
	}

	newIndexDesc, newIndexJSON, err := generateIndex(updated)
	if err != nil {
		return err
	}
	if err := s.archive.SaveBlob(ctx, ociblob.NewDescriptorBlob(io.NopCloser(bytes.NewReader(newIndexJSON)), newIndexDesc)); err != nil {
		return fmt.Errorf("unable to save referrers index for referrers tag %q: %w", referrersTag, err)
	}

	hadPriorIndex := !content.Equal(oldIndexDesc, ociImageSpecV1.Descriptor{})

	// Clearing the old tag before creating the new one is kind of a hack. This
	// allows garbage collection of the old index blobs without having to
	// extend index API.
	if hadPriorIndex {
		if err := idx.RemoveTag(s.repo, referrersTag); err != nil && !errors.Is(err, v1.ErrArtifactNotFound) {
			return fmt.Errorf("unable to remove prior referrers tag %q: %w", referrersTag, err)
		}
	}

	// tag the new referrer index with the referrers tag schema.
	if err := s.applyTag(ctx, idx, newIndexDesc, referrersTag); err != nil {
		return fmt.Errorf("unable to retag referrers index for referrers tag %q: %w", referrersTag, err)
	}

	// best-effort GC of the prior referrers index blob. The RemoveTag above
	// dropped the only index entry; nothing else points at this blob.
	if hadPriorIndex {
		if err := s.archive.DeleteBlob(ctx, oldIndexDesc.Digest.String()); err != nil {
			slog.DebugContext(ctx, "failed to delete stale referrers index blob",
				"digest", oldIndexDesc.Digest.String(), "referrersTag", referrersTag, "error", err)
		}
	}
	return nil
}

// referrerFromManifest inspects a pushed manifest for a subject field and, if
// present, returns the subject together with the referrer descriptor enriched
// the way the Referrers API response requires: artifactType set (falling back
// to the config media type for image manifests) and annotations copied from
// the manifest.
// A nil subject is returned for manifests without one and for media types
// that do not define a subject field (e.g. Docker manifests).
//
// Inspired by oras remote.indexReferrersForPush. Essentially identical to the
// original function, but returns instead of calling updateReferrersIndex
// itself. This fits our control flow better.
func referrerFromManifest(desc ociImageSpecV1.Descriptor, manifestJSON []byte) (referrer ociImageSpecV1.Descriptor, subject *ociImageSpecV1.Descriptor, err error) {
	switch desc.MediaType {
	case ociImageSpecV1.MediaTypeImageManifest:
		var manifest ociImageSpecV1.Manifest
		if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
			return desc, nil, fmt.Errorf("failed to decode manifest %s: %s: %w", desc.Digest, desc.MediaType, err)
		}
		if manifest.Subject == nil {
			// no subject, no indexing needed
			return desc, nil, nil
		}
		desc.ArtifactType = manifest.ArtifactType
		if desc.ArtifactType == "" {
			// https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#listing-referrers
			desc.ArtifactType = manifest.Config.MediaType
		}
		desc.Annotations = manifest.Annotations
		return desc, manifest.Subject, nil
	case ociImageSpecV1.MediaTypeImageIndex:
		var index ociImageSpecV1.Index
		if err := json.Unmarshal(manifestJSON, &index); err != nil {
			return desc, nil, fmt.Errorf("failed to decode index %s: %s: %w", desc.Digest, desc.MediaType, err)
		}
		if index.Subject == nil {
			// no subject, no indexing needed
			return desc, nil, nil
		}
		// indexes have no config; an empty artifactType stays empty per spec.
		desc.ArtifactType = index.ArtifactType
		desc.Annotations = index.Annotations
		return desc, index.Subject, nil
	default:
		return desc, nil, nil
	}
}

type referrerKey struct {
	mediaType string
	digest    digest.Digest
	size      int64
}

func referrerKeyOf(d ociImageSpecV1.Descriptor) referrerKey {
	return referrerKey{mediaType: d.MediaType, digest: d.Digest, size: d.Size}
}

// addReferrer adds a referrer to a list of referrers.
// Returns the updated referrers list and a boolean indicating if the list
// was changed.
//
// Inspired by oras remote.applyReferrerChanges.
func addReferrer(referrers []ociImageSpecV1.Descriptor, referrer ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, bool) {
	// clean up - deduplicate and remove empty entries
	seen := make(map[referrerKey]struct{}, len(referrers)+1)
	updated := make([]ociImageSpecV1.Descriptor, 0, len(referrers)+1)
	changed := false
	for _, r := range referrers {
		if content.Equal(r, ociImageSpecV1.Descriptor{}) {
			// skip bad entry
			changed = true
			continue
		}
		if _, ok := seen[referrerKeyOf(r)]; ok {
			// skip duplicates
			changed = true
			continue
		}
		seen[referrerKeyOf(r)] = struct{}{}
		updated = append(updated, r)
	}

	// add referrer if not already present
	if _, ok := seen[referrerKeyOf(referrer)]; !ok {
		updated = append(updated, referrer)
		changed = true
	}
	return updated, changed
}

// generateIndex generates an image index containing the given manifests list.
//
// Copied from oras remote.generateIndex.
func generateIndex(manifests []ociImageSpecV1.Descriptor) (ociImageSpecV1.Descriptor, []byte, error) {
	if manifests == nil {
		manifests = []ociImageSpecV1.Descriptor{} // make it an empty array to prevent potential server-side bugs
	}
	index := ociImageSpecV1.Index{
		Versioned: specs.Versioned{
			SchemaVersion: 2, // historical value. does not pertain to OCI or docker version
		},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: manifests,
	}
	indexJSON, err := json.Marshal(index)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to marshal referrers index: %w", err)
	}
	indexDesc := content.NewDescriptorFromBytes(index.MediaType, indexJSON)
	return indexDesc, indexJSON, nil
}

// filterReferrers filters a slice of referrers by artifactType in place.
// The returned slice contains matching referrers.
//
// Copied from oras remote.filterReferrers.
func filterReferrers(refs []ociImageSpecV1.Descriptor, artifactType string) []ociImageSpecV1.Descriptor {
	if artifactType == "" {
		return refs
	}
	var j int
	for i, ref := range refs {
		if ref.ArtifactType == artifactType {
			if i != j {
				refs[j] = ref
			}
			j++
		}
	}
	return refs[:j]
}
