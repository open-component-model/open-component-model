package transformer

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCIArtifactTransformer extracts OCI artifacts from blob data.
// It can interpret any blob as an OCI Layout and extract main artifacts.
type OCIArtifactTransformer struct{}

// NewOCIArtifactTransformer creates a new OCIArtifactTransformer instance.
func NewOCIArtifactTransformer() *OCIArtifactTransformer {
	return &OCIArtifactTransformer{}
}

// TransformBlob transforms an OCI Layout blob by extracting its main artifacts.
// This provides generic OCI artifact extraction for any media type.
func (t *OCIArtifactTransformer) TransformBlob(ctx context.Context, input blob.ReadOnlyBlob, _ runtime.Typed) (_ blob.ReadOnlyBlob, err error) {
	store, err := ocitar.ReadOCILayout(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to read OCI layout: %w", err)
	}
	defer func() {
		err = errors.Join(err, store.Close())
	}()

	mainArtifacts := store.MainArtifacts(ctx)
	if len(mainArtifacts) != 1 {
		return nil, fmt.Errorf("should have exactly one main artifact but was %d", len(mainArtifacts))
	}

	artifact := mainArtifacts[0]

	extractedBlob, err := extractOCIArtifact(ctx, store, artifact, t.processLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to extract artifact layers: %w", err)
	}

	return extractedBlob, nil
}

// processLayer processes a single layer from the OCI manifest.
func (t *OCIArtifactTransformer) processLayer(ctx context.Context, store content.Fetcher, layer ociImageSpecV1.Descriptor, tarWriter *tar.Writer) (err error) {
	layerReader, err := store.Fetch(ctx, layer)
	if err != nil {
		return fmt.Errorf("failed to fetch layer: %w", err)
	}
	defer func() {
		err = errors.Join(err, layerReader.Close())
	}()

	filename := t.getFilenameForMediaType(layer.MediaType)
	header := &tar.Header{
		Name: filename,
		Size: layer.Size,
		Mode: 0o644,
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write TAR header: %w", err)
	}

	if _, err := io.Copy(tarWriter, layerReader); err != nil {
		return fmt.Errorf("failed to copy layer data: %w", err)
	}

	return nil
}

// getFilenameForMediaType determines the appropriate filename based on media type.
func (t *OCIArtifactTransformer) getFilenameForMediaType(mediaType string) string {
	// For unknown media types, try to extract a reasonable filename
	if strings.Contains(mediaType, "tar") {
		if strings.Contains(mediaType, "gzip") {
			return "layer.tar.gz"
		}
		return "layer.tar"
	}
	if strings.Contains(mediaType, "json") {
		return "layer.json"
	}
	// Default to generic layer name
	return "layer.bin"
}
