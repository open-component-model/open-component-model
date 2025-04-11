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
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

func WithCTF(archive *Store) oci.RepositoryOption {
	return func(options *oci.RepositoryOptions) {
		options.Resolver = archive
	}
}

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

// StoreForReference returns a new Store instance for a specific repository within the CTF archive.
func (s *Store) StoreForReference(_ context.Context, reference string) (spec.Store, error) {
	rawRef, err := s.Reference(reference)
	if err != nil {
		return nil, err
	}
	ref := rawRef.(looseref.LooseReference)

	return &repositoryStore{
		archive:  s.archive,
		registry: ref.Registry,
		repo:     ref.Repository,
	}, nil
}

func (s *Store) Reference(reference string) (fmt.Stringer, error) {
	return looseref.ParseReference(reference)
}

// ComponentVersionReference creates a reference string for a component version in the format "component-descriptors/component:version".
func (s *Store) ComponentVersionReference(component, version string) string {
	return fmt.Sprintf("component-descriptors/%s:%s", component, version)
}

// repositoryStore implements the spec.Store interface for a CTF archive specific to a repository.
type repositoryStore struct {
	archive  ctf.CTF
	registry string
	repo     string
	indexMu  sync.RWMutex
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
	if err := s.archive.SaveBlob(ctx, ociblob.NewDescriptorBlob(io.NopCloser(data), expected)); err != nil {
		return fmt.Errorf("unable to save blob for descriptor %v: %w", expected, err)
	}
	if isManifest(expected) {
		if err := s.Tag(ctx, expected, expected.Digest.String()); err != nil {
			return fmt.Errorf("unable to save manifest for descriptor %v: %w", expected, err)
		}
	}

	return nil
}

// Resolve resolves a reference string to its corresponding descriptor in the CTF archive.
// The reference should be in the format "repository:tag" so it will be resolved against the index.
// If a full reference is given, it will be resolved against the blob repositoryStore immediately.
// Returns the descriptor if found, or an error if the reference is invalid or not found.
func (s *repositoryStore) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	var b blob.ReadOnlyBlob

	s.indexMu.RLock()
	defer s.indexMu.RUnlock()

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("unable to get index: %w", err)
	}
	repo := s.repo
	if s.registry != "" {
		repo = s.registry + "/" + repo
	}

	if strings.HasPrefix(reference, repo+":") {
		reference = strings.TrimPrefix(reference, repo+":")
	}

	for _, artifact := range idx.GetArtifacts() {
		if artifact.Repository != repo {
			continue
		}
		if !(artifact.Tag == reference || artifact.Digest == reference) {
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

	if b, err := s.archive.GetBlob(ctx, reference); err == nil {
		return ociImageSpecV1.Descriptor{
			MediaType: "application/octet-stream",
			Digest:    digest.Digest(reference),
			Size:      b.(blob.SizeAware).Size(),
		}, nil
	}

	return ociImageSpecV1.Descriptor{}, errdef.ErrNotFound
}

// Tag associates a descriptor with a reference in the CTF archive's index.
// The reference should be in the format "repository:tag".
// This operation updates the index to maintain the mapping between references and their corresponding descriptors.
func (s *repositoryStore) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return fmt.Errorf("unable to get index: %w", err)
	}

	repo := s.repo
	if s.registry != "" {
		repo = s.registry + "/" + repo
	}
	ref := registry.Reference{Reference: reference}

	var meta v1.ArtifactMetadata
	if err := ref.ValidateReferenceAsTag(); err == nil {
		meta = v1.ArtifactMetadata{
			Repository: repo,
			Tag:        reference,
			Digest:     desc.Digest.String(),
			MediaType:  desc.MediaType,
		}
	} else if err := ref.ValidateReferenceAsDigest(); err == nil {
		meta = v1.ArtifactMetadata{
			Repository: repo,
			Digest:     desc.Digest.String(),
			MediaType:  desc.MediaType,
		}
	} else {
		return fmt.Errorf("invalid reference %q: %w", reference, err)
	}

	slog.Info("adding artifact to index", "meta", meta)

	addOrUpdateArtifactMetadataInIndex(idx, meta)

	if err := s.archive.SetIndex(ctx, idx); err != nil {
		return fmt.Errorf("unable to set index: %w", err)
	}
	return nil
}

func (s *repositoryStore) Tags(ctx context.Context, _ string, fn func(tags []string) error) error {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return fmt.Errorf("unable to get index: %w", err)
	}

	arts := idx.GetArtifacts()
	if len(arts) == 0 {
		return nil
	}

	repo := s.repo
	if s.registry != "" {
		repo = s.registry + "/" + repo
	}

	tags := make([]string, 0, len(arts))
	for _, art := range arts {
		if art.Repository != repo {
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
		if art.Repository == meta.Repository && art.Digest == meta.Digest && art.MediaType == meta.MediaType {
			arts[i] = meta
			found = true
			break
		}
	}
	if !found {
		idx.AddArtifact(meta)
	}
}

func isManifest(desc ociImageSpecV1.Descriptor) bool {
	switch desc.MediaType {
	case ociImageSpecV1.MediaTypeImageManifest,
		ociImageSpecV1.MediaTypeImageIndex:
		return true
	default:
		return false
	}
}
