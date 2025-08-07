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
	"ocm.software/open-component-model/bindings/go/configuration/extract/v1alpha1/spec"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Transformer extracts OCI artifacts with media-type specific handling.
type Transformer struct{}

// New creates a new OCI artifact transformer.
func New() *Transformer {
	return &Transformer{}
}

// TransformBlob transforms an OCI Layout blob by extracting its main artifacts.
func (t *Transformer) TransformBlob(ctx context.Context, input blob.ReadOnlyBlob, config runtime.Typed) (_ blob.ReadOnlyBlob, err error) {
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
	var extractConfig *spec.Config
	if config != nil {
		if cfg, ok := config.(*spec.Config); ok {
			extractConfig = cfg
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

	// If no config provided, extract all layers with default naming
	if config == nil || len(config.Rules) == 0 {
		for i, layer := range manifest.Layers {
			filename := t.getDefaultFilename(layer.MediaType, i)
			if err := t.processLayer(ctx, store, layer, tarWriter, filename); err != nil {
				return nil, fmt.Errorf("failed to process layer %s: %w", layer.Digest, err)
			}
		}
	} else {
		// Process layers according to configured rules
		for _, rule := range config.Rules {
			if err := t.processRule(ctx, store, manifest.Layers, rule, tarWriter); err != nil {
				return nil, fmt.Errorf("failed to process rule for file %s: %w", rule.Filename, err)
			}
		}
	}

	resultBlob := inmemory.New(bytes.NewReader(tarBuffer.Bytes()))
	resultBlob.SetMediaType("application/tar")

	return resultBlob, nil
}

// processRule processes layers that match the rule's selectors into a single tar file.
func (t *Transformer) processRule(ctx context.Context, store content.Fetcher, layers []ociImageSpecV1.Descriptor, rule spec.Rule, tarWriter *tar.Writer) error {
	for i, layer := range layers {
		layerInfo := spec.LayerInfo{
			Index:       i,
			MediaType:   layer.MediaType,
			Annotations: layer.Annotations,
		}

		// Check if this layer matches any of the rule's selectors
		matched := false
		for _, selector := range rule.LayerSelectors {
			if selector.Matches(layerInfo) {
				matched = true
				break
			}
		}

		if matched {
			// if we have a filename, use it, otherwise use default based on media type
			filename := rule.Filename
			if filename == "" {
				filename = t.getDefaultFilename(layer.MediaType, i)
			}

			if err := t.processLayer(ctx, store, layer, tarWriter, filename); err != nil {
				return fmt.Errorf("failed to process layer %s: %w", layer.Digest, err)
			}
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
func (t *Transformer) getDefaultFilename(mediaType string, index int) string {
	if strings.Contains(mediaType, "tar") {
		if strings.Contains(mediaType, "gzip") {
			return fmt.Sprintf("layer-%d.tar.gz", index)
		}
		return fmt.Sprintf("layer-%d.tar", index)
	}
	if strings.Contains(mediaType, "json") {
		return fmt.Sprintf("layer-%d.json", index)
	}

	return fmt.Sprintf("layer-%d.bin", index)
}
