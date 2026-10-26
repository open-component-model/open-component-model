package oci

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/ctf"
)

// convertArtifactSetToOCILayout converts legacy OCM artifact sets before the normal OCI
// layout upload path. Non-artifact-set blobs pass through unchanged.
func (repo *Repository) convertArtifactSetToOCILayout(ctx context.Context, b blob.ReadOnlyBlob) (blob.ReadOnlyBlob, func() error, error) {
	mediaTypeAware, ok := b.(blob.MediaTypeAware)
	if !ok {
		return b, nil, nil
	}
	mediaType, known := mediaTypeAware.MediaType()
	if !known || !ctf.IsArtifactSetMediaType(mediaType) {
		return b, nil, nil
	}

	artifactSet, err := ctf.NewArtifactSetFromBlob(b)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read artifact set: %w", err)
	}

	file, err := os.CreateTemp(repo.tempDir, "ocm-artifact-set-*.tar")
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("failed to create temporary OCI layout: %w", err), artifactSet.Close())
	}
	cleanup := func() error {
		return errors.Join(file.Close(), os.Remove(file.Name()))
	}

	convertErr := ctf.ConvertToOCIImageLayout(ctx, artifactSet, file, func(_ context.Context, _ digest.Digest, oldName string) (string, error) {
		return oldName, nil
	})
	if err := errors.Join(convertErr, artifactSet.Close()); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("failed to convert artifact set to OCI layout: %w", err), cleanup())
	}

	return direct.New(file), cleanup, nil
}
