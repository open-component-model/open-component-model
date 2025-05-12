package io

import (
	"context"
	"encoding/json"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor/input/helm/helmlite"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

func ReadHelmOCILayout(ctx context.Context, b blob.ReadOnlyBlob) (*helmlite.Chart, error) {
	layout, err := tar.ReadOCILayout(ctx, b)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = layout.Close()
	}()

	if len(layout.Index.Manifests) > 1 {
		return nil, fmt.Errorf("multiple manifests found in OCI layout, cannot determine which one to read from")
	}

	manifest := layout.Index.Manifests[0]

	return ReadHelmChartFromOCILayoutTar(ctx, layout, manifest)
}

func ReadHelmChartFromOCILayoutTar(ctx context.Context, store *tar.CloseableReadOnlyStore, desc ociImageSpecV1.Descriptor) (*helmlite.Chart, error) {
	manifestData, err := store.Fetch(ctx, desc)
	if err != nil {
		return nil, fmt.Errorf("error fetching manifest data: %w", err)
	}
	defer func() {
		_ = manifestData.Close()
	}()

	var manifest ociImageSpecV1.Manifest
	if err := json.NewDecoder(manifestData).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("error decoding manifest data: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("no layers found in manifest")
	}

	var contentLayer ociImageSpecV1.Descriptor
	for _, layer := range manifest.Layers {
		if layer.MediaType == helmlite.ChartLayerMediaType {
			contentLayer = layer
			break
		}
	}
	if contentLayer.Digest == "" {
		return nil, fmt.Errorf("no content layer found in manifest")
	}
	// Read the content layer
	contentLayerData, err := store.Fetch(ctx, contentLayer)
	if err != nil {
		return nil, fmt.Errorf("error fetching content layer: %w", err)
	}
	defer func() {
		_ = contentLayerData.Close()
	}()

	// Read the chart data from the manifest
	chart, err := helmlite.LoadArchive(contentLayerData)

	return chart, err
}
