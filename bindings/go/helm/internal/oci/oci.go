package oci

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/helm/internal"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// Result holds both the OCI layout blob and the manifest descriptor produced by CopyChartToOCILayout.
type Result struct {
	Blob *filesystem.Blob
	Desc *ociImageSpecV1.Descriptor
}

// CopyChartToOCILayout takes a ChartData helper object and creates an OCI layout from it.
// Three OCI layers are expected: config, tgz contents and optionally a provenance file.
// The result is tagged with the helm chart version.
// The OCI layout is written to a temporary file in dir and returned as a file-backed blob.
// See also: https://github.com/helm/community/blob/main/hips/hip-0006.md#2-support-for-provenance-files
func CopyChartToOCILayout(ctx context.Context, chart *internal.ChartData, dir string) (*Result, error) {
	tmpFile, err := os.CreateTemp(dir, "oci-layout-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file for OCI layout: %w", err)
	}
	defer func(tmpFile *os.File) {
		_ = tmpFile.Close()
	}(tmpFile)

	zippedBuf := gzip.NewWriter(tmpFile)

	target := tar.NewOCILayoutWriter(zippedBuf)

	// Generate and push layers based on the chart to the OCI layout.
	configLayer, chartLayer, provLayer, err := pushChartAndGenerateLayers(ctx, chart, target)
	if err != nil {
		return nil, fmt.Errorf("failed to push chart layers: %w", err)
	}

	layers := []ociImageSpecV1.Descriptor{*chartLayer}
	if provLayer != nil {
		layers = append(layers, *provLayer)
	}

	// Create OCI image manifest.
	imgDesc, err := oras.PackManifest(ctx, target, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{
		ConfigDescriptor: configLayer,
		Layers:           layers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI image manifest: %w", err)
	}

	if err := target.Tag(ctx, imgDesc, chart.Version); err != nil {
		return nil, fmt.Errorf("failed to tag OCI image: %w", err)
	}

	if err := errors.Join(target.Close(), zippedBuf.Close()); err != nil {
		return nil, fmt.Errorf("failed to finalize OCI layout: %w", err)
	}

	blob, err := filesystem.GetBlobFromOSPath(tmpFile.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to create blob from OCI layout file: %w", err)
	}
	blob.SetMediaType(layout.MediaTypeOCIImageLayoutTarGzipV1)

	return &Result{Blob: blob, Desc: &imgDesc}, nil
}

func pushChartAndGenerateLayers(ctx context.Context, chart *internal.ChartData, target oras.Target) (
	configLayer *ociImageSpecV1.Descriptor,
	chartLayer *ociImageSpecV1.Descriptor,
	provLayer *ociImageSpecV1.Descriptor,
	err error,
) {
	// Create config OCI layer.
	if configLayer, err = pushConfigLayer(ctx, chart.Name, chart.Version, target); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create and push helm chart config layer: %w", err)
	}

	// Create Helm Chart OCI layer.
	if chartLayer, err = pushChartLayer(ctx, chart.ChartBlob, target); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create and push helm chart content layer: %w", err)
	}

	// Create Provenance OCI layer (optional).
	if chart.ProvBlob != nil {
		if provLayer, err = pushProvenanceLayer(ctx, chart.ProvBlob, target); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create and push helm chart provenance: %w", err)
		}
	}
	return configLayer, chartLayer, provLayer, err
}

func pushConfigLayer(ctx context.Context, name, version string, target oras.Target) (_ *ociImageSpecV1.Descriptor, err error) {
	type chartConfig struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}

	config := chartConfig{Name: name, Version: version}
	jsonConfig, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal helm chart config to JSON: %w", err)
	}

	configLayer := &ociImageSpecV1.Descriptor{
		MediaType: registry.ConfigMediaType,
		Digest:    digest.FromBytes(jsonConfig),
		Size:      int64(len(jsonConfig)),
	}
	if err = target.Push(ctx, *configLayer, bytes.NewReader(jsonConfig)); err != nil {
		return nil, fmt.Errorf("failed to push helm chart config layer: %w", err)
	}
	return configLayer, nil
}

func pushProvenanceLayer(ctx context.Context, provenance *filesystem.Blob, target oras.Target) (_ *ociImageSpecV1.Descriptor, err error) {
	provDigStr, known := provenance.Digest()
	if !known {
		return nil, fmt.Errorf("unknown digest for helm provenance")
	}
	provenanceReader, err := provenance.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to get a reader for helm chart provenance: %w", err)
	}
	defer func() {
		err = errors.Join(err, provenanceReader.Close())
	}()

	provenanceLayer := ociImageSpecV1.Descriptor{
		MediaType: registry.ProvLayerMediaType,
		Digest:    digest.Digest(provDigStr),
		Size:      provenance.Size(),
	}
	if err = target.Push(ctx, provenanceLayer, provenanceReader); err != nil {
		return nil, fmt.Errorf("failed to push helm chart provenance layer: %w", err)
	}

	return &provenanceLayer, nil
}

func pushChartLayer(ctx context.Context, chart *filesystem.Blob, target oras.Target) (_ *ociImageSpecV1.Descriptor, err error) {
	if chart == nil {
		return nil, fmt.Errorf("chart blob must not be nil")
	}
	// We get the reader first because Digest only returns a boolean and no error.
	// This hides errors like, "file not found" or "permission denied" on downloaded content.
	chartReader, err := chart.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to get a reader for helm chart blob: %w", err)
	}
	defer func() {
		err = errors.Join(err, chartReader.Close())
	}()

	chartDigStr, known := chart.Digest()
	if !known {
		return nil, fmt.Errorf("unknown digest for helm chart")
	}

	chartLayer := ociImageSpecV1.Descriptor{
		MediaType: registry.ChartLayerMediaType,
		Digest:    digest.Digest(chartDigStr),
		Size:      chart.Size(),
	}
	if err = target.Push(ctx, chartLayer, chartReader); err != nil {
		return nil, fmt.Errorf("failed to push helm chart content layer: %w", err)
	}

	return &chartLayer, nil
}
