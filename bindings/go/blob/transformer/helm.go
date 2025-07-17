package transformer

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
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

// HelmTransformer is a specialized transformer for Helm OCI artifacts.
type HelmTransformer struct{}

// NewHelmTransformer creates a new Helm transformer instance.
func NewHelmTransformer() *HelmTransformer {
	return &HelmTransformer{}
}

// TransformBlob transforms a Helm OCI artifact by extracting its contents.
// TODO: This will need extract.oci.artifact.ocm.software config type.
func (t *HelmTransformer) TransformBlob(ctx context.Context, input blob.ReadOnlyBlob, _ runtime.Typed) (blob.ReadOnlyBlob, error) {
	store, err := ocitar.ReadOCILayout(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to read OCI layout: %w", err)
	}
	defer store.Close()

	mainArtifacts := store.MainArtifacts(ctx)
	if len(mainArtifacts) == 0 {
		return nil, fmt.Errorf("no main artifacts found in OCI layout")
	}

	// TODO: This will be configurable.
	artifact := mainArtifacts[0]

	extractedBlob, err := t.extractHelmChart(ctx, store, artifact)
	if err != nil {
		return nil, fmt.Errorf("failed to extract Helm chart: %w", err)
	}

	return extractedBlob, nil
}

// extractHelmChart extracts Helm chart layers from an OCI artifact.
func (t *HelmTransformer) extractHelmChart(ctx context.Context, store content.Fetcher, artifact ociImageSpecV1.Descriptor) (blob.ReadOnlyBlob, error) {
	manifestReader, err := store.Fetch(ctx, artifact)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch artifact manifest: %w", err)
	}
	defer manifestReader.Close()

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

	for _, layer := range manifest.Layers {
		if err := t.processHelmLayer(ctx, store, layer, tarWriter); err != nil {
			tarWriter.Close()
			return nil, fmt.Errorf("failed to process layer %s: %w", layer.Digest, err)
		}
	}

	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close TAR writer: %w", err)
	}

	resultBlob := inmemory.New(bytes.NewReader(tarBuffer.Bytes()))
	resultBlob.SetMediaType("application/tar")

	return resultBlob, nil
}

// processHelmLayer processes a single layer from the Helm OCI manifest.
func (t *HelmTransformer) processHelmLayer(ctx context.Context, store content.Fetcher, layer ociImageSpecV1.Descriptor, tarWriter *tar.Writer) error {
	layerReader, err := store.Fetch(ctx, layer)
	if err != nil {
		return fmt.Errorf("failed to fetch layer: %w", err)
	}
	defer layerReader.Close()

	filename := t.getHelmFilename(layer.MediaType)

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

// getHelmFilename determines the appropriate filename for Helm-specific media types.
func (t *HelmTransformer) getHelmFilename(mediaType string) string {
	switch mediaType {
	case MediaTypeHelmChart:
		return "chart.tar.gz"
	case MediaTypeHelmProvenance:
		return "chart.prov"
	case MediaTypeHelmConfig:
		return "config.json"
	default:
		// For unknown media types within Helm context, provide reasonable defaults
		if strings.Contains(mediaType, "tar") {
			if strings.Contains(mediaType, "gzip") {
				return "helm-content.tar.gz"
			}
			return "helm-content.tar"
		}
		if strings.Contains(mediaType, "json") {
			return "helm-config.json"
		}
		if strings.Contains(mediaType, "prov") {
			return "helm-provenance.prov"
		}
		return "helm-layer.bin"
	}
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
