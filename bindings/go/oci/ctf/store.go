package ctf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
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
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

// wellKnownRegistryCTF is the well-known registry for CTF archives that is set by default when resolving references.
// it is a relative domain that is resolved in the context of the CTF archive and is equivalent to not setting a domain.
// it can be used to differentiate multi-slash paths and registries. as an example
//
//   - ctf.ocm.software/component-descriptors/repo => registry:=ctf.ocm.software, repository=component-descriptors/repo
//   - component-descriptors/repo:tag => registry=component-descriptors, repository=repo
const wellKnownRegistryCTF = "ctf.ocm.software"

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

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// Store implements an OCI Store interface backed by a CTF (Common Transport Format).
// It provides functionality to:
// - Resolve and Tag component version references using the CTF's index archive
// - Handle blob operations (Fetch, Exists, Push) through the CTF's blob archive
// - Emulate an OCM OCI Repository for accessing component versions stored in the CTF
type Store struct {
	// mu protects concurrent access to the CTF archive, especially for index
	// operations. A pointer to the mutex is shared with all oras oci store
	// instances created for repositories within this CTF to ensure consistent
	// locking.
	mu      sync.RWMutex
	archive ctf.CTF
}

// Ping for CTF return always true. This is because if it doesn't exist it will be created. If it does exist
// it's all good. Which means it doesn't make any sense to check it.
func (s *Store) Ping(ctx context.Context) error {
	return nil
}

// StoreForReference returns a new Store instance for a specific repository within the CTF archive.
func (s *Store) StoreForReference(_ context.Context, reference string) (spec.Store, error) {
	rawRef, err := looseref.ParseReference(reference)
	if err != nil {
		return nil, err
	}

	return &repository{
		mu:      &s.mu,
		archive: s.archive,
		repo:    rawRef.Repository,
	}, nil
}

// ComponentVersionReference creates a reference string for a component version in the format "component-descriptors/component:version".
func (s *Store) ComponentVersionReference(ctx context.Context, component, version string) string {
	tag := oci.LooseSemverToOCITag(ctx, version) // Remove prohibited characters.
	return fmt.Sprintf("%s/component-descriptors/%s:%s", wellKnownRegistryCTF, component, tag)
}

// Repository implements the spec.Store interface for a CTF OCI Repository.
// Each repository instance shares the same mutex with its parent Store to ensure
// consistent locking across all repositories within the same CTF archive.
type repository struct {
	archive ctf.CTF
	repo    string
	mu      *sync.RWMutex
}

// Fetch retrieves a blob from the CTF archive based on its descriptor.
// Returns an io.ReadCloser for the blob content or an error if the blob cannot be found.
// Uses LockedReader to maintain read lock during async streaming operations. The io.ReadCloser must be closed.
func (s *repository) Fetch(ctx context.Context, target ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	s.mu.RLock()
	b, err := s.archive.GetBlob(ctx, target.Digest.String())
	if err != nil {
		s.mu.RUnlock()
		return nil, err
	}
	rc, err := b.ReadCloser()
	if err != nil {
		s.mu.RUnlock()
		return nil, err
	}
	return s.asLockedReader(rc), nil
}

func (s *repository) asLockedReader(rc io.ReadCloser) io.ReadCloser {
	isFirstCall := true
	doClose := sync.OnceValue(func() error {
		isFirstCall = false
		defer s.mu.RUnlock()
		return rc.Close()
	})

	return struct {
		io.Reader
		io.Closer
	}{
		Reader: rc,
		Closer: closerFunc(func() error {
			if !isFirstCall {
				slog.Error("Close called multiple times on locked reader.")
			}
			return doClose()
		}),
	}
}

// Exists checks if a blob exists in the CTF archive based on its descriptor.
// Returns true if the blob exists, false otherwise.
func (s *repository) Exists(ctx context.Context, target ociImageSpecV1.Descriptor) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exists(ctx, target)
}

// exists is the internal version of Exists that assumes the caller holds the lock.
func (s *repository) exists(ctx context.Context, target ociImageSpecV1.Descriptor) (bool, error) {
	blobs, err := s.archive.ListBlobs(ctx)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("unable to list blobs: %w", err)
	}
	return slices.Contains(blobs, target.Digest.String()), nil
}

func (s *repository) FetchReference(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, io.ReadCloser, error) {
	s.mu.RLock()

	desc, err := s.resolve(ctx, reference)
	if err != nil {
		s.mu.RUnlock()
		return ociImageSpecV1.Descriptor{}, nil, err
	}

	b, err := s.archive.GetBlob(ctx, desc.Digest.String())
	if err != nil {
		s.mu.RUnlock()
		return ociImageSpecV1.Descriptor{}, nil, err
	}
	rc, err := b.ReadCloser()
	if err != nil {
		s.mu.RUnlock()
		return ociImageSpecV1.Descriptor{}, nil, err
	}
	return desc, s.asLockedReader(rc), nil
}

// Push stores a new blob in the CTF archive with the expected descriptor.
// The content is read from the provided io.Reader.
func (s *repository) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, data io.Reader) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.archive.SaveBlob(ctx, ociblob.NewDescriptorBlob(io.NopCloser(data), expected)); err != nil {
		return fmt.Errorf("unable to save blob for descriptor %v: %w", expected, err)
	}
	if introspection.IsOCICompliantManifest(expected) {
		if err := s.tag(ctx, expected, expected.Digest.String()); err != nil {
			return fmt.Errorf("unable to save manifest for descriptor %v: %w", expected, err)
		}
	}

	return nil
}

// Resolve resolves a reference string to its corresponding descriptor in the CTF archive.
// The reference should be in the format "repository:tag" so it will be resolved against the index.
// The reference can also be just a tag or a digest, in which case the repository is based on the base repository.
// Alternatively, it is also possible to provide a digest directly, e.g., "sha256:abc123...".
// If a full reference is given, it will be resolved against the blob Repository immediately.
// Returns the descriptor if found, or an error if the reference is invalid or not found.
func (s *repository) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolve(ctx, reference)
}

// resolve is the internal version of Resolve that assumes the caller holds the lock.
func (s *repository) resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	var b blob.ReadOnlyBlob

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("unable to get index: %w", err)
	}

	repo := s.repo

	// We need to parse the reference loosely because the registry/repository information may be prefixed to the actual reference.
	if ref, err := looseref.ParseReference(reference); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("invalid reference %q: %w", reference, err)
	} else {
		// If we have a valid digest or tag, use it instead of the reference.
		if refOrTag := ref.ReferenceOrTag(); refOrTag != "" {
			reference = refOrTag
		}
	}

	for _, artifact := range idx.GetArtifacts() {
		if artifact.Repository != repo {
			continue
		}
		if artifact.Tag != reference && artifact.Digest != reference {
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
			artifact.MediaType = ociImageSpecV1.MediaTypeImageManifest
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

	slog.DebugContext(ctx, "reference not found in index", "repository", repo, "reference", reference)
	return ociImageSpecV1.Descriptor{}, errdef.ErrNotFound
}

// Tag associates a descriptor with a reference in the CTF archive's index.
// The reference should be in the format "repository:tag", but can also be just a tag or digest.
// This operation updates the index to maintain the mapping between references and their corresponding descriptors.
func (s *repository) Tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tag(ctx, desc, reference)
}

// tag is the internal version of Tag that assumes the caller holds the lock.
func (s *repository) tag(ctx context.Context, desc ociImageSpecV1.Descriptor, reference string) error {
	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return fmt.Errorf("unable to get index: %w", err)
	}

	repo := s.repo

	var meta v1.ArtifactMetadata
	if ref, err := looseref.ParseReference(reference); err == nil {
		if err := ref.ValidateReferenceAsTag(); err == nil {
			meta = v1.ArtifactMetadata{
				Repository: repo,
				Tag:        ref.Tag,
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
			ref := registry.Reference{Reference: reference}
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
				return fmt.Errorf("invalid raw reference %q: %w", reference, err)
			}
		}
	}

	ok, err := s.exists(ctx, desc)
	if err != nil {
		return fmt.Errorf("unable to check if descriptor exists: %w", err)
	}
	if !ok {
		// if the descriptor does not exist, we cannot tag it
		return fmt.Errorf("descriptor %s does not exist in the archive", desc.Digest)
	}

	slog.DebugContext(ctx, "adding artifact to index", "meta", meta)

	addOrUpdateArtifactMetadataInIndex(idx, meta)

	if err := s.archive.SetIndex(ctx, idx); err != nil {
		return fmt.Errorf("unable to set index: %w", err)
	}
	return nil
}

func (s *repository) Tags(ctx context.Context, _ string, fn func(tags []string) error) error {
	s.mu.RLock()

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		s.mu.RUnlock()
		return fmt.Errorf("unable to get index: %w", err)
	}

	arts := idx.GetArtifacts()
	if len(arts) == 0 {
		s.mu.RUnlock()
		return nil
	}

	repo := s.repo

	tags := make([]string, 0, len(arts))
	for _, art := range arts {
		if art.Repository != repo {
			continue
		}
		// This is check is required because in Tag, we omit the tag completely
		// in case it is a valid digest.
		if art.Tag == "" {
			continue
		}
		tags = append(tags, art.Tag)
	}

	// Unlock before invoking the callback to avoid potential re-entrant locking deadlocks.
	s.mu.RUnlock()
	return fn(tags)
}

func addOrUpdateArtifactMetadataInIndex(idx v1.Index, meta v1.ArtifactMetadata) {
	arts := idx.GetArtifacts()

	// check if the tag already exists within the repository
	// if it does, we need to nil out the old tag if the digest differs, (thats equivalent to a retag)
	for i, art := range arts {
		tagAlreadyExists := art.Repository == meta.Repository && art.Tag == meta.Tag
		digestDiffers := art.Digest != meta.Digest
		if tagAlreadyExists && digestDiffers {
			arts[i].Tag = ""
			break
		}
	}

	idx.AddArtifact(meta)
}
