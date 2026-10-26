package oci

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/opencontainers/go-digest"
	slogcontext "github.com/veqryn/slog-context"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/ctf"
)

// isArtifactSetBlob reports whether b is a legacy OCM artifact set that must be
// converted to an OCI layout (see convertArtifactSetToOCILayout) before upload.
func isArtifactSetBlob(b blob.ReadOnlyBlob) bool {
	mediaTypeAware, ok := b.(blob.MediaTypeAware)
	if !ok {
		return false
	}
	mediaType, known := mediaTypeAware.MediaType()
	return known && ctf.IsArtifactSetMediaType(mediaType)
}

// convertArtifactSetToOCILayout converts a legacy OCM artifact set to an OCI layout
// for the normal upload path. The caller must ensure b is an artifact set (see
// isArtifactSetBlob); the returned cleanup removes the temporary layout.
func (repo *Repository) convertArtifactSetToOCILayout(ctx context.Context, b blob.ReadOnlyBlob) (blob.ReadOnlyBlob, func() error, error) {
	artifactSet, err := ctf.NewArtifactSetFromBlob(b)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read artifact set: %w", err)
	}

	var mediaType string
	if mediaTypeAware, ok := b.(blob.MediaTypeAware); ok {
		mediaType, _ = mediaTypeAware.MediaType()
	}
	slogcontext.Info(ctx, "converting legacy artifact set to OCI layout for upload",
		slog.String("mediaType", mediaType), slog.String("format", artifactSet.Format().String()))

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
