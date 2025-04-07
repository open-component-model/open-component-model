package oci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/ctf"
	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
	"ocm.software/open-component-model/bindings/go/oci"
)

var _ registry.TagLister = (*Store)(nil)

// NewFromCTF creates a new Store instance that wraps a CTF (Common Transport Format) archive.
// This ctf.CTF archive acts as an OCI repository interface for component versions stored in the CTF.
func NewFromCTF(store ctf.CTF) *Store {
	return &Store{archive: store}
}

// Store implements an OCI Store interface backed by a CTF (Common Transport Format).
// It provides functionality to:
// - Resolve and Tag component version references using the CTF's index archive
// - Handle blob operations (Fetch, Exists, Push) through the CTF's blob archive
// - Emulate an OCM OCI Repository for accessing component versions stored in the CTF
type Store struct {
	archive ctf.CTF
	refLock sync.Mutex
}

// TargetResourceReference returns the source reference as-is, as this archive doesn't modify references.
func (s *Store) TargetResourceReference(srcReference string) (string, error) {
	return srcReference, nil
}

// StoreForReference returns the current archive instance for any reference, as this archive handles all operations.
func (s *Store) StoreForReference(context.Context, string) (oci.Store, error) {
	return s, nil
}

// ComponentVersionReference creates a reference string for a component version in the format "component-descriptors/component:version".
func (s *Store) ComponentVersionReference(component, version string) string {
	return fmt.Sprintf("component-descriptors/%s:%s", component, version)
}

// ParseReference parses an OCI reference
// It is a special form of registry.ParseReference which
// adds a default registry prefix if the reference is missing a registry or repository.
// This is because CTF stores do not necessarily need a registry URL context (as they are local archives).
func ParseReference(reference string) (resolved registry.Reference, err error) {
	ref, err := registry.ParseReference(reference)
	if err != nil && strings.Contains(err.Error(), "missing registry or repository") {
		ref, err = registry.ParseReference(fmt.Sprintf("CTF/%s", reference))
	}
	ref.Registry = ""
	return ref, err
}

// Fetch retrieves a blob from the CTF archive based on its descriptor.
// Returns an io.ReadCloser for the blob content or an error if the blob cannot be found.
func (s *Store) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	b, err := s.archive.GetBlob(ctx, target.Digest.String())
	if err != nil {
		return nil, fmt.Errorf("unable to get blob: %w", err)
	}
	return b.ReadCloser()
}

// Exists checks if a blob exists in the CTF archive based on its descriptor.
// Returns true if the blob exists, false otherwise.
func (s *Store) Exists(ctx context.Context, target ociImageSpecV1.Descriptor) (bool, error) {
	blobs, err := s.archive.ListBlobs(ctx)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("unable to list blobs: %w", err)
	}
	return slices.Contains(blobs, target.Digest.String()), nil
}

func (r *Store) FetchReference(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, io.ReadCloser, error) {
	desc, err := r.Resolve(ctx, reference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, err
	}
	data, err := r.Fetch(ctx, desc)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, err
	}
	return desc, data, nil
}

// Push stores a new blob in the CTF archive with the expected descriptor.
// The content is read from the provided io.Reader.
func (s *Store) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, data io.Reader) error {
	if err := s.archive.SaveBlob(ctx, oci.NewDescriptorBlob(data, expected)); err != nil {
		return fmt.Errorf("unable to save blob for descriptor %v: %w", expected, err)
	}

	// switch expected.MediaType {
	// case ociImageSpecV1.MediaTypeImageManifest, ociImageSpecV1.MediaTypeImageIndex:
	// 	manifestJSON, err := content.ReadAll(data, expected)
	// 	if err != nil {
	// 		return fmt.Errorf("unable to read manifest JSON: %w", err)
	// 	}
	// 	if err := s.indexReferrersForPush(ctx, expected, manifestJSON); err != nil {
	// 		return fmt.Errorf("unable to index referrers for push: %w", err)
	// 	}
	// }

	return nil
}

// Resolve resolves a reference string to its corresponding descriptor in the CTF archive.
// The reference should be in the format "repository:tag" so it will be resolved against the index.
// If a full reference is given, it will be resolved against the blob store immediately.
// Returns the descriptor if found, or an error if the reference is invalid or not found.
func (s *Store) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	ref, err := ParseReference(reference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}

	var b blob.ReadOnlyBlob

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("unable to get index: %w", err)
	}

	for _, artifact := range idx.GetArtifacts() {
		if artifact.Repository != ref.Repository || artifact.Tag != ref.Reference {
			continue
		}

		var size int64
		if b, err = s.archive.GetBlob(ctx, artifact.Digest); err == nil {
			if sizeAware, ok := b.(blob.SizeAware); ok {
				size = sizeAware.Size()
			}
		} else {
			return ociImageSpecV1.Descriptor{}, err
		}

		// old CTFs do not have a mediaType field set at all.
		// we can thus assume that any CTF we encounter in the wild that does not have this media type field
		// is actually a CTF generated with OCMv1. in this case we know it is an embedded ArtifactSet
		if artifact.MediaType == "" {
			artifact.MediaType = ctf.ArtifactSetMediaType
		}

		return ociImageSpecV1.Descriptor{
			MediaType: artifact.MediaType,
			Digest:    digest.Digest(artifact.Digest),
			Size:      size,
		}, nil
	}

	return ociImageSpecV1.Descriptor{}, errdef.ErrNotFound
}

// Tag associates a descriptor with a reference in the CTF archive's index.
// The reference should be in the format "repository:tag".
// This operation updates the index to maintain the mapping between references and their corresponding descriptors.
func (s *Store) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
	ref, err := ParseReference(reference)
	if err != nil {
		return err
	}

	if isTag := ref.ValidateReferenceAsTag() == nil; !isTag {
		return fmt.Errorf("invalid reference, must be a valid taggable reference: %s", reference)
	}

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return fmt.Errorf("unable to get index: %w", err)
	}

	meta := v1.ArtifactMetadata{
		Repository: ref.Repository,
		Tag:        ref.Reference,
		Digest:     desc.Digest.String(),
		MediaType:  desc.MediaType,
	}

	slog.Info("tagging artifact in index", "meta", meta)

	addOrUpdateArtifactMetadataInIndex(idx, meta)

	if err := s.archive.SetIndex(ctx, idx); err != nil {
		return fmt.Errorf("unable to set index: %w", err)
	}
	return nil
}

// func (s *Store) Referrers(ctx context.Context, desc ociImageSpecV1.Descriptor, artifactType string, fn func(referrers []ociImageSpecV1.Descriptor) error) error {
// 	idx, err := s.archive.GetIndex(ctx)
// 	if err != nil {
// 		return fmt.Errorf("unable to get index: %w", err)
// 	}
//
// 	arts := idx.GetArtifacts()
// 	if len(arts) == 0 {
// 		return nil
// 	}
//
// 	tag := buildReferrersTag(desc)
// }

func (s *Store) Tags(ctx context.Context, _ string, fn func(tags []string) error) error {
	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return fmt.Errorf("unable to get index: %w", err)
	}

	arts := idx.GetArtifacts()
	if len(arts) == 0 {
		return nil
	}

	tags := make([]string, len(arts))
	for i, art := range arts {
		tags[i] = art.Tag
	}

	return fn(tags)
}

func addOrUpdateArtifactMetadataInIndex(idx v1.Index, meta v1.ArtifactMetadata) {
	arts := idx.GetArtifacts()
	var found bool
	for i, art := range arts {
		if art.Repository == meta.Repository && art.Tag == meta.Tag {
			arts[i] = meta
			found = true
			break
		}
	}
	if !found {
		idx.AddArtifact(meta)
	}
}

// // buildReferrersTag builds the referrers tag for the given manifest descriptor.
// // Format: <algorithm>-<digest>
// // Reference: https://github.com/opencontainers/distribution-spec/blob/v1.1.0/spec.md#unavailable-referrers-api
// func buildReferrersTag(desc ociImageSpecV1.Descriptor) string {
// 	alg := desc.Digest.Algorithm().String()
// 	encoded := desc.Digest.Encoded()
// 	return alg + "-" + encoded
// }

// // errNoReferrerUpdate is returned by applyReferrerChanges() when there
// // is no any referrer update.
// var errNoReferrerUpdate = errors.New("no referrer update")
//
// // updateReferrersIndex updates the referrers index for desc referencing subject
// // on manifest push and manifest delete.
// // References:
// //   - https://github.com/opencontainers/distribution-spec/blob/v1.1.0/spec.md#pushing-manifests-with-subject
// //   - https://github.com/opencontainers/distribution-spec/blob/v1.1.0/spec.md#deleting-manifests
// func (s *Store) updateReferrersIndex(ctx context.Context, subject ociImageSpecV1.Descriptor, desc ociImageSpecV1.Descriptor) (err error) {
// 	s.refLock.Lock()
// 	defer s.refLock.Unlock()
// 	referrersTag := buildReferrersTag(subject)
//
// 	var oldIndexDesc *ociImageSpecV1.Descriptor
// 	var oldReferrers []ociImageSpecV1.Descriptor
// 	prepare := func() error {
// 		// 1. pull the original referrers list using the referrers tag schema
// 		indexDesc, referrers, err := s.referrersFromIndex(ctx, referrersTag)
// 		if err != nil {
// 			if errors.Is(err, errdef.ErrNotFound) {
// 				// valid case: no old referrers index
// 				return nil
// 			}
// 			return err
// 		}
// 		oldIndexDesc = &indexDesc
// 		oldReferrers = referrers
// 		return nil
// 	}
// 	update := func(referrerChanges []ociImageSpecV1.Descriptor) error {
// 		// 2. apply the referrer changes on the referrers list
// 		updatedReferrers, err := applyReferrerChanges(oldReferrers, referrerChanges)
// 		if errors.Is(err, errNoReferrerUpdate) {
// 			return nil
// 		}
// 		if err != nil {
// 			return err
// 		}
//
// 		// 3. push the updated referrers list using referrers tag schema
// 		if len(updatedReferrers) > 0 {
// 			// push a new index in either case:
// 			// 1. the referrers list has been updated with a non-zero size
// 			// 2. OR the updated referrers list is empty but referrers GC
// 			//    is skipped, in this case an empty index should still be pushed
// 			//    as the old index won't get deleted
// 			newIndexDesc, newIndex, err := generateIndex(updatedReferrers)
// 			if err != nil {
// 				return fmt.Errorf("failed to generate referrers index for referrers tag %s: %w", referrersTag, err)
// 			}
// 			if err := s.Push(ctx, newIndexDesc, bytes.NewReader(newIndex)); err != nil {
// 				return fmt.Errorf("failed to push referrers index: %w", err)
// 			}
// 			if err := s.Tag(ctx, newIndexDesc, referrersTag); err != nil {
// 				return fmt.Errorf("failed to push referrers index tagged by %s: %w", referrersTag, err)
// 			}
// 		}
//
// 		// 4. delete the dangling original referrers index, if applicable
// 		if oldIndexDesc == nil {
// 			return nil
// 		}
// 		if err := s.repo.delete(ctx, *oldIndexDesc, true); err != nil {
// 			return &ReferrersError{
// 				Op:      opDeleteReferrersIndex,
// 				Err:     fmt.Errorf("failed to delete dangling referrers index %s for referrers tag %s: %w", oldIndexDesc.Digest.String(), referrersTag, err),
// 				Subject: subject,
// 			}
// 		}
// 		return nil
// 	}
//
// 	merge, done := s.repo.referrersMergePool.Get(referrersTag)
// 	defer done()
// 	return merge.Do(change, prepare, update)
// }
//
// func (s *Store) indexReferrersForPush(ctx context.Context, desc ociImageSpecV1.Descriptor, manifestJSON []byte) error {
// 	var subject ociImageSpecV1.Descriptor
// 	switch desc.MediaType {
// 	case ociImageSpecV1.MediaTypeImageManifest:
// 		var manifest ociImageSpecV1.Manifest
// 		if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
// 			return fmt.Errorf("failed to decode manifest: %s: %s: %w", desc.Digest, desc.MediaType, err)
// 		}
// 		if manifest.Subject == nil {
// 			// no subject, no indexing needed
// 			return nil
// 		}
// 		subject = *manifest.Subject
// 		desc.ArtifactType = manifest.ArtifactType
// 		if desc.ArtifactType == "" {
// 			desc.ArtifactType = manifest.Config.MediaType
// 		}
// 		desc.Annotations = manifest.Annotations
// 	case ociImageSpecV1.MediaTypeImageIndex:
// 		var manifest ociImageSpecV1.Index
// 		if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
// 			return fmt.Errorf("failed to decode manifest: %s: %s: %w", desc.Digest, desc.MediaType, err)
// 		}
// 		if manifest.Subject == nil {
// 			// no subject, no indexing needed
// 			return nil
// 		}
// 		subject = *manifest.Subject
// 		desc.ArtifactType = manifest.ArtifactType
// 		desc.Annotations = manifest.Annotations
// 	default:
// 		return nil
// 	}
//
// 	return s.updateReferrersIndex(ctx, subject, desc)
// }
// func (r *Store) referrersFromIndex(ctx context.Context, referrersTag string) (ociImageSpecV1.Descriptor, []ociImageSpecV1.Descriptor, error) {
// 	desc, rc, err := r.FetchReference(ctx, referrersTag)
// 	if err != nil {
// 		return ociImageSpecV1.Descriptor{}, nil, err
// 	}
// 	defer rc.Close()
//
// 	var index ociImageSpecV1.Index
// 	if err := decodeJSON(rc, desc, &index); err != nil {
// 		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to decode referrers index from referrers tag %s: %w", referrersTag, err)
// 	}
//
// 	return desc, index.Manifests, nil
// }
//
// // decodeJSON safely reads the JSON content described by desc, and
// // decodes it into v.
// func decodeJSON(r io.Reader, desc ociImageSpecV1.Descriptor, v any) error {
// 	jsonBytes, err := content.ReadAll(r, desc)
// 	if err != nil {
// 		return err
// 	}
// 	return json.Unmarshal(jsonBytes, v)
// }
//
// // applyReferrerChanges applies referrerChanges on referrers and returns the
// // updated referrers.
// // Returns errNoReferrerUpdate if there is no any referrers updates.
// func applyReferrerChanges(referrers []ociImageSpecV1.Descriptor, referrerChanges []ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
// 	referrersMap := make(map[Descriptor]int, len(referrers)+len(referrerChanges))
// 	updatedReferrers := make([]ociImageSpecV1.Descriptor, 0, len(referrers)+len(referrerChanges))
// 	var updateRequired bool
// 	for _, r := range referrers {
// 		if content.Equal(r, ociImageSpecV1.Descriptor{}) {
// 			// skip bad entry
// 			updateRequired = true
// 			continue
// 		}
// 		key := FromOCI(r)
// 		if _, ok := referrersMap[key]; ok {
// 			// skip duplicates
// 			updateRequired = true
// 			continue
// 		}
// 		updatedReferrers = append(updatedReferrers, r)
// 		referrersMap[key] = len(updatedReferrers) - 1
// 	}
//
// 	// apply changes
// 	for _, change := range referrerChanges {
// 		key := FromOCI(change)
// 		if _, ok := referrersMap[key]; !ok {
// 			// add distinct referrers
// 			updatedReferrers = append(updatedReferrers, change)
// 			referrersMap[key] = len(updatedReferrers) - 1
// 		}
// 	}
//
// 	// skip unnecessary update
// 	if !updateRequired && len(referrersMap) == len(referrers) {
// 		// if the result referrer map contains the same content as the
// 		// original referrers, consider that there is no update on the
// 		// referrers.
// 		for _, r := range referrers {
// 			key := FromOCI(r)
// 			if _, ok := referrersMap[key]; !ok {
// 				updateRequired = true
// 			}
// 		}
// 		if !updateRequired {
// 			return nil, errNoReferrerUpdate
// 		}
// 	}
//
// 	return removeEmptyDescriptors(updatedReferrers, len(referrersMap)), nil
// }
//
// // removeEmptyDescriptors in-place removes empty items from descs, given a hint
// // of the number of non-empty descriptors.
// func removeEmptyDescriptors(descs []ociImageSpecV1.Descriptor, hint int) []ociImageSpecV1.Descriptor {
// 	j := 0
// 	for i, r := range descs {
// 		if !content.Equal(r, ociImageSpecV1.Descriptor{}) {
// 			if i > j {
// 				descs[j] = r
// 			}
// 			j++
// 		}
// 		if j == hint {
// 			break
// 		}
// 	}
// 	return descs[:j]
// }
//
// // Descriptor contains the minimun information to describe the disposition of
// // targeted content.
// // Since it only has strings and integers, Descriptor is a comparable struct.
// type Descriptor struct {
// 	// MediaType is the media type of the object this schema refers to.
// 	MediaType string `json:"mediaType,omitempty"`
//
// 	// Digest is the digest of the targeted content.
// 	Digest digest.Digest `json:"digest"`
//
// 	// Size specifies the size in bytes of the blob.
// 	Size int64 `json:"size"`
// }
//
// // Empty is an empty descriptor
// var Empty Descriptor
//
// // FromOCI shrinks the OCI descriptor to the minimum.
// func FromOCI(desc ociImageSpecV1.Descriptor) Descriptor {
// 	return Descriptor{
// 		MediaType: desc.MediaType,
// 		Digest:    desc.Digest,
// 		Size:      desc.Size,
// 	}
// }
//
// // generateIndex generates an image index containing the given manifests list.
// func generateIndex(manifests []ociImageSpecV1.Descriptor) (ociImageSpecV1.Descriptor, []byte, error) {
// 	if manifests == nil {
// 		manifests = []ociImageSpecV1.Descriptor{} // make it an empty array to prevent potential server-side bugs
// 	}
// 	index := ociImageSpecV1.Index{
// 		Versioned: specs.Versioned{
// 			SchemaVersion: 2, // historical value. does not pertain to OCI or docker version
// 		},
// 		MediaType: ociImageSpecV1.MediaTypeImageIndex,
// 		Manifests: manifests,
// 	}
// 	indexJSON, err := json.Marshal(index)
// 	if err != nil {
// 		return ociImageSpecV1.Descriptor{}, nil, err
// 	}
// 	indexDesc := content.NewDescriptorFromBytes(index.MediaType, indexJSON)
// 	return indexDesc, indexJSON, nil
// }
