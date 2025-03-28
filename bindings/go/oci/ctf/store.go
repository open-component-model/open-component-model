package oci

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/ctf"
	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
	"ocm.software/open-component-model/bindings/go/oci"
)

// WithCTF is an OCI repository option that allows you to specify a CTF (Common Transport Format) archive as the resolver.
func WithCTF(ctf ctf.CTF) oci.RepositoryOption {
	return func(o *oci.RepositoryOptions) {
		o.Resolver = NewFromCTF(ctf)
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

// parseReference splits a reference string into repository and tag parts.
// Returns an error if the reference format is invalid.
func parseReference(reference string) (repo, tag string, err error) {
	parts := strings.SplitN(reference, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid reference: %s", reference)
	}
	return parts[0], parts[1], nil
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
	b, err := s.archive.GetBlob(ctx, target.Digest.String())
	return b != nil && err == nil, nil
}

// Push stores a new blob in the CTF archive with the expected descriptor.
// The content is read from the provided io.Reader.
func (s *Store) Push(ctx context.Context, expected ociImageSpecV1.Descriptor, content io.Reader) error {
	return s.archive.SaveBlob(ctx, oci.NewDescriptorBlob(content, expected))
}

// Resolve resolves a reference string to its corresponding descriptor in the CTF archive.
// The reference should be in the format "repository:tag".
// Returns the descriptor if found, or an error if the reference is invalid or not found.
func (s *Store) Resolve(ctx context.Context, reference string) (ociImageSpecV1.Descriptor, error) {
	repo, tag, err := parseReference(reference)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("unable to get index: %w", err)
	}

	for _, artifact := range idx.GetArtifacts() {
		if artifact.Repository != repo || artifact.Tag != tag {
			continue
		}

		var size int64
		if b, err := s.archive.GetBlob(ctx, artifact.Digest); err == nil {
			if sizeAware, ok := b.(blob.SizeAware); ok {
				size = sizeAware.Size()
			}
		}

		return ociImageSpecV1.Descriptor{
			MediaType: ociImageSpecV1.MediaTypeImageManifest,
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
	repo, tag, err := parseReference(reference)
	if err != nil {
		return err
	}

	idx, err := s.archive.GetIndex(ctx)
	if err != nil {
		return fmt.Errorf("unable to get index: %w", err)
	}

	meta := v1.ArtifactMetadata{
		Repository: repo,
		Tag:        tag,
		Digest:     desc.Digest.String(),
	}
	slog.Info("tagging artifact in index", "meta", meta)

	idx.AddArtifact(meta)
	if err := s.archive.SetIndex(ctx, idx); err != nil {
		return fmt.Errorf("unable to set index: %w", err)
	}
	return nil
}
