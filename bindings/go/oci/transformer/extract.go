package transformer

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Helm OCI artifact media types as defined in HIP-0006
const (
	MediaTypeHelmChart      = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	MediaTypeHelmProvenance = "application/vnd.cncf.helm.chart.provenance.v1.prov"
	MediaTypeHelmConfig     = "application/vnd.cncf.helm.config.v1+json"
)

// Transformer extracts OCI artifacts with media-type specific handling.
type Transformer struct{}

// New creates a new OCI artifact transformer.
func New() *Transformer {
	return &Transformer{}
}

// TransformBlob transforms an OCI Layout blob by extracting its main artifacts.
func (t *Transformer) TransformBlob(ctx context.Context, input blob.ReadOnlyBlob, _ runtime.Typed) (_ blob.ReadOnlyBlob, err error) {
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
	return t.extractOCIArtifact(ctx, store, artifact)
}

// extractOCIArtifact extracts all layers from an OCI artifact into a tar archive.
func (t *Transformer) extractOCIArtifact(ctx context.Context, store content.Fetcher, artifact ociImageSpecV1.Descriptor) (_ blob.ReadOnlyBlob, err error) {
	manifestReader, err := store.Fetch(ctx, artifact)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch artifact manifest: %w", err)
	}
	defer func() {
		err = errors.Join(err, manifestReader.Close())
	}()

	manifestData, err := io.ReadAll(manifestReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest data: %w", err)
	}

	var manifest ociImageSpecV1.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}

	var tarBuffer bytes.Buffer
	tarWriter := tar.NewWriter(&tarBuffer)
	defer func() {
		err = errors.Join(err, tarWriter.Close())
	}()

	for _, layer := range manifest.Layers {
		if err := t.processLayer(ctx, store, layer, tarWriter); err != nil {
			return nil, fmt.Errorf("failed to process layer %s: %w", layer.Digest, err)
		}
	}

	resultBlob := inmemory.New(bytes.NewReader(tarBuffer.Bytes()))
	resultBlob.SetMediaType("application/tar")

	return resultBlob, nil
}

// processLayer processes a single layer from the OCI manifest with media-type specific filename handling.
func (t *Transformer) processLayer(ctx context.Context, store content.Fetcher, layer ociImageSpecV1.Descriptor, tarWriter *tar.Writer) (err error) {
	layerReader, err := store.Fetch(ctx, layer)
	if err != nil {
		return fmt.Errorf("failed to fetch layer: %w", err)
	}
	defer func() {
		err = errors.Join(err, layerReader.Close())
	}()

	filename := t.GetFilename(layer.MediaType)

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

// GetFilename determines the appropriate filename based on media type.
func (t *Transformer) GetFilename(mediaType string) string {
	// Handle Helm-specific media types
	switch mediaType {
	case MediaTypeHelmChart:
		return "chart.tar.gz"
	case MediaTypeHelmProvenance:
		return "chart.prov"
	case MediaTypeHelmConfig:
		return "config.json"
	}

	// Generic OCI layer handling
	if strings.Contains(mediaType, "tar") {
		if strings.Contains(mediaType, "gzip") {
			return "layer.tar.gz"
		}
		return "layer.tar"
	}
	if strings.Contains(mediaType, "json") {
		return "layer.json"
	}

	return "layer.bin"
}

// IsHelmMediaType checks if a media type is Helm-specific.
func IsHelmMediaType(mediaType string) bool {
	switch mediaType {
	case MediaTypeHelmChart, MediaTypeHelmProvenance, MediaTypeHelmConfig:
		return true
	default:
		return false
	}
}
