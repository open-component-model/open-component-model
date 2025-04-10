package oci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/ctf"
	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

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
}

// TargetResourceReference returns the source reference as-is, as this archive doesn't modify references.
func (s *Store) TargetResourceReference(srcReference string) (string, error) {
	return srcReference, nil
}

// StoreForReference returns a new Store instance for a specific repository within the CTF archive.
func (s *Store) StoreForReference(_ context.Context, reference string) (spec.Store, error) {
	ref, err := looseref.LooseParseReference(reference)
	if err != nil {
		return nil, err
	}

	return &repositoryStore{
		archive: s.archive,
		repo:    ref.Repository,
	}, nil
}

// ComponentVersionReference creates a reference string for a component version in the format "component-descriptors/component:version".
func (s *Store) ComponentVersionReference(component, version string) string {
	return fmt.Sprintf("component-descriptors/%s:%s", component, version)
}

// repositoryStore implements the spec.Store interface for a CTF archive specific to a repository.
type repositoryStore struct {
	archive ctf.CTF
	repo    string
}

// Fetch retrieves a blob from the CTF archive based on its descriptor.
// Returns an io.ReadCloser for the blob content or an error if the blob cannot be found.
func (s *repositoryStore) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	b, err := s.archive.GetBlob(ctx, target.Digest.String())
	if err != nil {
		return nil, fmt.Errorf("unable to get blob: %w", err)
	}
	return b.ReadCloser()
}

// Exists checks if a blob exists in the CTF archive based on its descriptor.
// Returns true if the blob exists, false otherwise.
func (s *repositoryStore) Exists(ctx context.Context, target ociImageSpecV1.Descriptor) (bool, error) {
	blobs, err := s.archive.ListBlobs(ctx)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("unable to list blobs: %w", err)
	}
	return slices.Contains(blobs, target.Digest.String()), nil
}

func (s *repositoryStore) FetchReference(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, io.ReadCloser, error) {
	desc, err := s.Resolve(ctx, reference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, err
	}
	data, err := s.Fetch(ctx, desc)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, err
	}
	return desc, data, nil
}

// Push stores a new blob in the CTF archive with the expected descriptor.
// The content is read from the provided io.Reader.
func (s *repositoryStore) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, data io.Reader) error {
	if err := s.archive.SaveBlob(ctx, ociblob.NewDescriptorBlob(data, expected)); err != nil {
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
// If a full reference is given, it will be resolved against the blob repositoryStore immediately.
// Returns the descriptor if found, or an error if the reference is invalid or not found.
func (s *repositoryStore) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	ref, err := looseref.LooseParseReference(reference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}

	var b blob.ReadOnlyBlob

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("unable to get index: %w", err)
	}

	for _, artifact := range idx.GetArtifacts() {
		if artifact.Repository != ref.Repository {
			continue
		}
		if artifact.Tag != ref.Tag {
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
func (s *repositoryStore) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
	ref, err := looseref.LooseParseReference(reference)
	if err != nil {
		return err
	}

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return fmt.Errorf("unable to get index: %w", err)
	}

	repo := s.repo

	if err := ref.ValidateReferenceAsTag(); err != nil {
		return fmt.Errorf("invalid reference (should have a tag) %q: %w", reference, err)
	}

	meta := v1.ArtifactMetadata{
		Repository: repo,
		Tag:        ref.Tag,
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

func (s *repositoryStore) Tags(ctx context.Context, _ string, fn func(tags []string) error) error {
	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return fmt.Errorf("unable to get index: %w", err)
	}

	arts := idx.GetArtifacts()
	if len(arts) == 0 {
		return nil
	}

	tags := make([]string, 0, len(arts))
	for _, art := range arts {
		if art.Repository != s.repo {
			continue
		}
		tags = append(tags, art.Tag)
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
