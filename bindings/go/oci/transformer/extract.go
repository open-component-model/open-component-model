package transformer

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/configuration/extract/v1alpha1/spec"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// Helm OCI media types
	helmConfigMediaType       = "application/vnd.cncf.helm.config.v1+json"
	helmChartContentMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	helmProvenanceMediaType   = "application/vnd.cncf.helm.chart.provenance.v1.prov"
)

// helmChartMetadata represents chart metadata from Helm config.
type helmChartMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Transformer extracts OCI artifacts with media-type specific handling.
type Transformer struct {
	logger *slog.Logger
}

// New creates a new OCI artifact transformer.
func New(logger *slog.Logger) *Transformer {
	return &Transformer{
		logger: logger,
	}
}

func (t *Transformer) GetTransformerScheme() *runtime.Scheme {
	return spec.Scheme
}

// TransformBlob transforms an OCI Layout blob by extracting its main artifacts.
func (t *Transformer) TransformBlob(ctx context.Context, input blob.ReadOnlyBlob, config runtime.Typed, _ map[string]string) (_ blob.ReadOnlyBlob, err error) {
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

	// Parse configuration
	extractConfig := &spec.Config{}
	if config != nil {
		if err := t.GetTransformerScheme().Convert(config, extractConfig); err != nil {
			return nil, fmt.Errorf("failed to convert config: %w", err)
		}
	}

	artifact := mainArtifacts[0]
	return t.extractOCIArtifact(ctx, store, artifact, extractConfig)
}

// extractOCIArtifact extracts selected layers from an OCI artifact into a tar archive.
func (t *Transformer) extractOCIArtifact(ctx context.Context, store content.Fetcher, artifact ociImageSpecV1.Descriptor, config *spec.Config) (_ blob.ReadOnlyBlob, err error) {
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

	// Helm is a special snowflake. The filename for the generated output needs to follow a specific naming convention.
	// The filename needs to be chartname-version.tgz for charts and chartname-version.tgz.prov for provenance.
	helmMetadata, err := t.extractHelmMetadata(ctx, store, manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to extract Helm metadata: %w", err)
	}

	// If no config provided, extract all layers with default naming
	if config == nil || len(config.Rules) == 0 {
		for _, layer := range manifest.Layers {
			filename := t.getFilenameForLayer(layer, helmMetadata)
			if err := t.processLayer(ctx, store, layer, tarWriter, filename); err != nil {
				return nil, fmt.Errorf("failed to process layer %s: %w", layer.Digest, err)
			}
		}
	} else {
		// Process layers according to configured rules
		for _, rule := range config.Rules {
			if err := t.processRuleWithHelm(ctx, store, manifest.Layers, rule, tarWriter, helmMetadata); err != nil {
				return nil, fmt.Errorf("failed to process rule for file %s: %w", rule.Filename, err)
			}
		}
	}

	resultBlob := inmemory.New(bytes.NewReader(tarBuffer.Bytes()))
	resultBlob.SetMediaType("application/tar")

	return resultBlob, nil
}

// processRuleWithHelm processes layers that match the rule's selectors into a single tar file, with Helm-aware filename handling.
func (t *Transformer) processRuleWithHelm(ctx context.Context, store content.Fetcher, layers []ociImageSpecV1.Descriptor, rule spec.Rule, tarWriter *tar.Writer, helmMetadata *helmChartMetadata) error {
	for i, layer := range layers {
		layerInfo := spec.LayerInfo{
			Index:       i,
			MediaType:   layer.MediaType,
			Annotations: layer.Annotations,
		}

		if !slices.ContainsFunc(rule.LayerSelectors, func(selector *spec.LayerSelector) bool {
			return selector.Matches(layerInfo)
		}) {
			continue
		}

		// For Helm layers (chart and provenance), ignore the configured filename and use Helm naming conventions
		// For non-Helm layers, use the configured filename or default
		var filename string
		if t.isHelmLayer(layer) && helmMetadata != nil {
			if rule.Filename != "" {
				t.logger.WarnContext(ctx, "filename for helm is generated based on config data, settings a filename will be ignored", "filename", rule.Filename)
			}
			filename = t.getFilenameForLayer(layer, helmMetadata)
		} else {
			filename = rule.Filename
			if filename == "" {
				filename = t.getDefaultFilename(layer.Digest.String())
			}
		}

		if err := t.processLayer(ctx, store, layer, tarWriter, filename); err != nil {
			return fmt.Errorf("failed to process layer %s: %w", layer.Digest, err)
		}
	}

	return nil
}

// processLayer processes a single layer from the OCI manifest.
func (t *Transformer) processLayer(ctx context.Context, store content.Fetcher, layer ociImageSpecV1.Descriptor, tarWriter *tar.Writer, filename string) (err error) {
	layerReader, err := store.Fetch(ctx, layer)
	if err != nil {
		return fmt.Errorf("failed to fetch layer: %w", err)
	}
	defer func() {
		err = errors.Join(err, layerReader.Close())
	}()

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

// getDefaultFilename provides fallback filename generation when no config is provided.
// Uses the layer's digest similar to how ORAS handles unnamed layers.
func (t *Transformer) getDefaultFilename(digest string) string {
	if strings.HasPrefix(digest, "sha256:") {
		return strings.TrimPrefix(digest, "sha256:")
	}
	switch {
	case strings.HasPrefix(digest, "sha512:"):
		return strings.TrimPrefix(digest, "sha512:")
	case strings.HasPrefix(digest, "sha256:"):
		return strings.TrimPrefix(digest, "sha256:")
	}

	// Fallback if digest's format is unexpected
	return digest
}

// extractHelmMetadata extracts chart metadata from the OCI config if this is a Helm chart.
func (t *Transformer) extractHelmMetadata(ctx context.Context, store content.Fetcher, manifest ociImageSpecV1.Manifest) (*helmChartMetadata, error) {
	if manifest.Config.MediaType != helmConfigMediaType {
		return nil, nil // Not a Helm chart
	}

	configReader, err := store.Fetch(ctx, manifest.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Helm config: %w", err)
	}
	defer configReader.Close()

	configData, err := io.ReadAll(configReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read Helm config data: %w", err)
	}

	var metadata helmChartMetadata
	if err := json.Unmarshal(configData, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Helm config: %w", err)
	}

	return &metadata, nil
}

// isHelmLayer checks if a layer contains Helm-related content (chart or provenance).
func (t *Transformer) isHelmLayer(layer ociImageSpecV1.Descriptor) bool {
	return layer.MediaType == helmChartContentMediaType || layer.MediaType == helmProvenanceMediaType
}

// getFilenameForLayer determines the appropriate filename for a layer, considering Helm naming conventions.
// https://helm.sh/docs/topics/charts/#charts-and-versioning details this requirement.
func (t *Transformer) getFilenameForLayer(layer ociImageSpecV1.Descriptor, helmMetadata *helmChartMetadata) string {
	if helmMetadata != nil {
		switch layer.MediaType {
		case helmChartContentMediaType:
			return fmt.Sprintf("%s-%s.tgz", helmMetadata.Name, helmMetadata.Version)
		case helmProvenanceMediaType:
			return fmt.Sprintf("%s-%s.tgz.prov", helmMetadata.Name, helmMetadata.Version)
		}
	}
	return t.getDefaultFilename(layer.Digest.String())
}
