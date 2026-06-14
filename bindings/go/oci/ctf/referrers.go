package ctf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"

	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
)

// A CTF archive is structurally a pre-v1.1 OCI registry: tags and digests in
// artifact-index.json, but no native index of manifests by their subject
// field. Referrer support is therefore implemented via the referrers tag
// schema that the OCI distribution spec defines as the fallback for exactly
// this situation, mirroring oras-go's remote.Repository behavior so that a
// CTF behaves like any other registry without Referrers API support:
//
//   - On push of a manifest with a subject (see [repository.Push]), the
//     referrers index stored under the tag "<alg>-<hex>" of the subject digest
//     is updated with the annotated referrer descriptor.
//   - On read (see [repository.Referrers]), that index is fetched and its
//     entries are returned, optionally filtered by artifact type.
//   - [repository.Predecessors] delegates to Referrers, so
//     oras.ExtendedCopyGraph picks referrers up as predecessors and copies
//     them alongside their subject.
//
// The referrers index itself is registry-local bookkeeping: it carries no
// subject edge, is never returned as a predecessor, and is consequently not
// copied by ExtendedCopyGraph. A destination store rebuilds its own index
// when the referrer manifests are pushed into it.
//
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#unavailable-referrers-api
//
// Concurrency: the spec notes that maintaining the referrers tag is the
// client's responsibility and that concurrent updates can lose data. Within
// a process, every repository derived from the same [Store] shares the
// store's write lock, which fully serializes the read-modify-write of the
// referrers index. Across multiple [Store] instances for the same path, or
// across processes, the CTF (like the rest of artifact-index.json handling)
// is single-writer; see also the store cache in the oci repository provider.

// maxManifestBytes caps how much manifest or referrers-index content is read
// into memory, both when buffering a pushed manifest to inspect its subject
// and when loading a referrers index. It matches oras-go's
// defaultMaxMetadataBytes.
const maxManifestBytes = 4 * 1024 * 1024 // 4 MiB

// Copied from oras: registry/remote/referrers.go
// buildReferrersTag builds the referrers tag for the given manifest descriptor.
// Format: <algorithm>-<digest>
// Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.1/spec.md#unavailable-referrers-api
func buildReferrersTag(desc ociImageSpecV1.Descriptor) (string, error) {
	if err := desc.Digest.Validate(); err != nil {
		return "", fmt.Errorf("failed to build referrers tag for %s: %w", desc.Digest, err)
	}
	alg := desc.Digest.Algorithm().String()
	encoded := desc.Digest.Encoded()
	return alg + "-" + encoded, nil
}

// Referrers lists the descriptors of manifests that directly reference desc
// via their subject field, looked up through the referrers tag schema.
// fn is called at most once with the (optionally artifactType-filtered)
// result; it is not called if there are no matching referrers.
//
// Like oras-go's remote.Repository, this is permissive about desc: any
// descriptor with a valid digest is accepted, and a missing referrers tag
// simply yields no referrers. Strict manifest checking is left to callers
// such as the package-level registry.Referrers helper.
//
// The lock is released before fn is invoked so that fn may safely call back
// into the store (e.g. to fetch referrer manifests), mirroring
// [repository.Tags].
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
	referrers, err := s.referrersFromArtifactIndex(ctx, idx, referrersTag)
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

// Predecessors returns the manifests in this repository that declare desc as
// their subject, resolved via the referrers tag schema. It delegates to
// [repository.Referrers], exactly like oras-go's remote.Repository: in a
// registry-shaped store the only predecessor edges are subject edges.
//
// This makes the CTF usable as a source for oras.ExtendedCopyGraph, whose
// default FindPredecessors is src.Predecessors: referrers (e.g. ADR 0016
// ownership referrers) ride along when their subject is copied out of the
// CTF.
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

// referrersFromArtifactIndex loads the referrers list stored under
// referrersTag, resolving the tag against the provided artifact-index
// snapshot rather than re-reading artifact-index.json. This lets
// [repository.Push] operate on the same snapshot it is about to mutate and
// write back.
//
// A missing tag entry or a dangling entry whose blob is gone is treated as
// "no referrers" — the next push self-heals by writing a fresh index. A
// present-but-undecodable index is an error, mirroring oras-go.
//
// The caller must hold s.mu (read or write).
func (s *repository) referrersFromArtifactIndex(ctx context.Context, idx v1.Index, referrersTag string) (referrers []ociImageSpecV1.Descriptor, err error) {
	desc, rc, err := s.fetchReference(ctx, idx, referrersTag)
	if errors.Is(err, errdef.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		// valid case: no referrers index for this subject, or the index
		// entry is dangling. Self-heal on the next push.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to fetch referrers index for referrers tag %q: %w", referrersTag, err)
	}
	defer func() {
		err = errors.Join(err, rc.Close())
	}()

	if desc.Size > maxManifestBytes {
		return nil, fmt.Errorf("referrers index %q for referrers tag %q exceeds size limit: %d > %d", desc.Digest, referrersTag, desc.Size, maxManifestBytes)
	}

	raw, err := io.ReadAll(io.LimitReader(rc, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("unable to read referrers index %q for referrers tag %q: %w", desc.Digest, referrersTag, err)
	}
	if len(raw) > maxManifestBytes {
		return nil, fmt.Errorf("referrers index %q for referrers tag %q exceeds size limit of %d bytes", desc.Digest, referrersTag, maxManifestBytes)
	}

	var refIdx ociImageSpecV1.Index
	if err := json.Unmarshal(raw, &refIdx); err != nil {
		return nil, fmt.Errorf("failed to decode referrers index from referrers tag %q: %w", referrersTag, err)
	}
	return refIdx.Manifests, nil
}

// updateReferrersIndex updates the referrers index for subject with referrer in the
// provided index snapshot, the write-side counterpart to
// [repository.Referrers] (analogous to oras-go's updateReferrersIndex with a
// single add operation). The caller must hold the write lock and persist the
// snapshot with SetIndex afterwards.
//
// Blobs are saved before the snapshot is persisted, so the referrers tag can
// never point at content that is not on disk; a crash in between leaves at
// worst a dangling blob, which is the documented CTF failure mode.
//
// No garbage collection is performed: when the referrers tag is moved to the
// new index, AddArtifact clears the previous entry's tag but the previous
// index blob remains (SkipReferrersGC semantics, matching the URL resolver).
// Deleting it would be unsafe anyway, because blobs in the flat CTF blob
// store are content-addressed and may be shared across repositories.
func (s *repository) updateReferrersIndex(ctx context.Context, idx v1.Index, subject, referrer ociImageSpecV1.Descriptor) error {
	referrersTag, err := buildReferrersTag(subject)
	if err != nil {
		return err
	}

	oldReferrers, err := s.referrersFromArtifactIndex(ctx, idx, referrersTag)
	if err != nil {
		return err
	}

	updated, changed := addReferrer(oldReferrers, referrer)
	if !changed {
		// the referrer is already indexed and the stored index is clean;
		// skip the write entirely, making referrer re-pushes idempotent.
		return nil
	}

	newIndexDesc, newIndexJSON, err := generateReferrersIndex(updated)
	if err != nil {
		return err
	}
	if err := s.archive.SaveBlob(ctx, ociblob.NewDescriptorBlob(io.NopCloser(bytes.NewReader(newIndexJSON)), newIndexDesc)); err != nil {
		return fmt.Errorf("unable to save referrers index for referrers tag %q: %w", referrersTag, err)
	}

	// digest entry for parity with regularly pushed manifests, so the
	// referrers index is resolvable and fetchable by digest like on a
	// registry.
	if err := s.applyTag(ctx, idx, newIndexDesc, newIndexDesc.Digest.String()); err != nil {
		return err
	}
	// the retag: AddArtifact clears the tag on the previous referrers index
	// entry (if any) and attaches it to the new one.
	if err := s.applyTag(ctx, idx, newIndexDesc, referrersTag); err != nil {
		return err
	}
	return nil
}

// artifactManifest is the subset of the deprecated OCI artifact manifest
// (introspection.MediaTypeArtifactManifest) needed for referrer indexing.
// oras-go/v2 defines the full type in internal/spec (not importable).
type artifactManifest struct {
	MediaType    string                     `json:"mediaType"`
	ArtifactType string                     `json:"artifactType,omitempty"`
	Subject      *ociImageSpecV1.Descriptor `json:"subject,omitempty"`
	Annotations  map[string]string          `json:"annotations,omitempty"`
}

// referrerFromManifest inspects a pushed manifest for a subject field and, if
// present, returns the subject together with the referrer descriptor enriched
// the way the Referrers API response requires: artifactType set (falling back
// to the config media type for image manifests) and annotations copied from
// the manifest. This mirrors oras-go's manifestStore.indexReferrersForPush
// and distribution-spec v1.1.1 "Listing Referrers".
//
// A nil subject is returned for manifests without one and for media types
// that do not define a subject field (e.g. Docker manifests).
func referrerFromManifest(desc ociImageSpecV1.Descriptor, manifestJSON []byte) (referrer ociImageSpecV1.Descriptor, subject *ociImageSpecV1.Descriptor, err error) {
	switch desc.MediaType {
	case ociImageSpecV1.MediaTypeImageManifest:
		var manifest ociImageSpecV1.Manifest
		if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
			return desc, nil, fmt.Errorf("failed to decode manifest %s: %s: %w", desc.Digest, desc.MediaType, err)
		}
		if manifest.Subject == nil {
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
			return desc, nil, nil
		}
		// indexes have no config; an empty artifactType stays empty per spec.
		desc.ArtifactType = index.ArtifactType
		desc.Annotations = index.Annotations
		return desc, index.Subject, nil
	case introspection.MediaTypeArtifactManifest:
		var manifest artifactManifest
		if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
			return desc, nil, fmt.Errorf("failed to decode artifact manifest %s: %s: %w", desc.Digest, desc.MediaType, err)
		}
		if manifest.Subject == nil {
			return desc, nil, nil
		}
		desc.ArtifactType = manifest.ArtifactType
		desc.Annotations = manifest.Annotations
		return desc, manifest.Subject, nil
	default:
		return desc, nil, nil
	}
}

// addReferrer returns the referrers list with referrer appended if it is not
// already present, deduplicated and cleansed of empty entries. The second
// return value reports whether the result differs from the input, i.e.
// whether a new referrers index needs to be written. This mirrors the
// semantics of oras-go's applyReferrerChanges for a single add operation:
// re-pushing an existing referrer is a no-op, but encountering bad or
// duplicate entries forces a rewrite so the stored index converges to a
// clean state.
//
// Descriptors are keyed by (mediaType, digest, size) like oras-go's
// descriptor.FromOCI; artifactType and annotations are derived from the
// manifest content and therefore covered by the digest.
func addReferrer(referrers []ociImageSpecV1.Descriptor, referrer ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, bool) {
	type key struct {
		mediaType string
		digest    digest.Digest
		size      int64
	}
	keyOf := func(d ociImageSpecV1.Descriptor) key {
		return key{mediaType: d.MediaType, digest: d.Digest, size: d.Size}
	}

	seen := make(map[key]struct{}, len(referrers)+1)
	updated := make([]ociImageSpecV1.Descriptor, 0, len(referrers)+1)
	changed := false
	for _, r := range referrers {
		if content.Equal(r, ociImageSpecV1.Descriptor{}) {
			// skip bad entry
			changed = true
			continue
		}
		if _, ok := seen[keyOf(r)]; ok {
			// skip duplicates
			changed = true
			continue
		}
		seen[keyOf(r)] = struct{}{}
		updated = append(updated, r)
	}

	if _, ok := seen[keyOf(referrer)]; !ok {
		updated = append(updated, referrer)
		changed = true
	}
	return updated, changed
}

// Copied from oras: registry/remote/repository.go (renamed as term index is
// very overloaded)
// generateReferrersIndex generates an image index containing the given manifests list.
func generateReferrersIndex(manifests []ociImageSpecV1.Descriptor) (ociImageSpecV1.Descriptor, []byte, error) {
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

// Copied from oras: registry/remote/referrers.go
// filterReferrers filters a slice of referrers by artifactType in place.
// The returned slice contains matching referrers.
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

// interface guard: the lister (internal/lister) selects referrer-based
// version listing by asserting registry.ReferrerLister on the store.
var _ registry.ReferrerLister = (*repository)(nil)
