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

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/ctf"
	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
	"ocm.software/open-component-model/bindings/go/oci"
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

// Push stores a new blob in the CTF archive with the expected descriptor.
// The content is read from the provided io.Reader.
func (s *Store) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, content io.Reader) error {
	if err := s.archive.SaveBlob(ctx, oci.NewDescriptorBlob(content, expected)); err != nil {
		return fmt.Errorf("unable to save blob for descriptor %v: %w", expected, err)
	}

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
