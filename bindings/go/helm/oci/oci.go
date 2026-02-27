package oci

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2"

	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/helm"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// Result holds both the OCI layout blob and the manifest descriptor produced by CopyChartToOCILayout.
//
// The manifest descriptor is computed inside a goroutine that streams data through an io.Pipe.
// That goroutine cannot finish (and thus cannot produce the descriptor) until the pipe is fully
// drained, i.e. the blob is consumed. Therefore the descriptor is only available after the blob
// has been fully read (e.g. written to disk). Calling Descriptor before that will block.
type Result struct {
	*direct.Blob
	desc chan descriptorOrError
}

type descriptorOrError struct {
	Descriptor ociImageSpecV1.Descriptor
	Err        error
}

// Descriptor returns the OCI image manifest descriptor.
// It blocks until the blob-producing goroutine has finished.
// The blob MUST be fully consumed before calling this method, otherwise it will deadlock.
func (r *Result) Descriptor() (ociImageSpecV1.Descriptor, error) {
	result := <-r.desc
	return result.Descriptor, result.Err
}

// CopyChartToOCILayout takes a ReadOnlyChart helper object and creates an OCI layout from it.
// Three OCI layers are expected: config, tgz contents and optionally a provenance file.
// The result is tagged with the helm chart version.
// The returned Result contains the blob and provides access to the manifest descriptor
// after the blob has been fully consumed.
// See also: https://github.com/helm/community/blob/main/hips/hip-0006.md#2-support-for-provenance-files
func CopyChartToOCILayout(ctx context.Context, chart *helm.ReadOnlyChart) *Result {
	// Why we cannot simply wait for the write to finish:
	// io.Pipe is unbuffered. The goroutine's writes block until someone reads from the other end.
	// If CopyChartToOCILayout waited for the goroutine to finish before returning, nobody would be reading the
	// pipe, so the goroutine would block on its first write. Deadlock.
	// We do not want to lose the steaming benefits of io.Pipe.
	r, w := io.Pipe()

	desc := make(chan descriptorOrError, 1)

	go copyChartToOCILayoutAsync(ctx, chart, w, desc)

	// TODO(ikhandamirov): replace this with a direct/unbuffered blob.
	return &Result{
		Blob: direct.New(r, direct.WithMediaType(layout.MediaTypeOCIImageLayoutTarGzipV1)),
		desc: desc,
	}
}

func copyChartToOCILayoutAsync(ctx context.Context, chart *helm.ReadOnlyChart, w *io.PipeWriter, descCh chan<- descriptorOrError) {
	// err accumulates any error from copy, gzip, or layout writing.
	var err error
	defer func() {
		_ = w.CloseWithError(err)            // Always returns nil.
		_ = os.RemoveAll(chart.ChartTempDir) // Always remove the created temp folder for the chart.
	}()

	zippedBuf := gzip.NewWriter(w)
	defer func() {
		err = errors.Join(err, zippedBuf.Close())
	}()

	// Create an OCI layout writer over the gzip stream.
	target := tar.NewOCILayoutWriter(zippedBuf)
	defer func() {
		err = errors.Join(err, target.Close())
	}()

	// Generate and Push layers based on the chart to the OCI layout.
	configLayer, chartLayer, provLayer, err := pushChartAndGenerateLayers(ctx, chart, target)
	if err != nil {
		err = fmt.Errorf("failed to push chart layers: %w", err)
		descCh <- descriptorOrError{Err: err}
		return
	}

	layers := []ociImageSpecV1.Descriptor{*chartLayer}
	if provLayer != nil {
		// If a provenance file was provided, add it to the layers.
		layers = append(layers, *provLayer)
	}

	// Create OCI image manifest.
	imgDesc, perr := oras.PackManifest(ctx, target, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{
		ConfigDescriptor: configLayer,
		Layers:           layers,
	})
	if perr != nil {
		err = fmt.Errorf("failed to create OCI image manifest: %w", perr)
		descCh <- descriptorOrError{Err: err}
		return
	}

	if terr := target.Tag(ctx, imgDesc, chart.Version); terr != nil {
		err = fmt.Errorf("failed to tag OCI image: %w", terr)
		descCh <- descriptorOrError{Err: err}
		return
	}

	descCh <- descriptorOrError{Descriptor: imgDesc}
}

func pushChartAndGenerateLayers(ctx context.Context, chart *helm.ReadOnlyChart, target oras.Target) (
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
	configContent := fmt.Sprintf(`{"name": "%s", "version": "%s"}`, name, version)
	configLayer := &ociImageSpecV1.Descriptor{
		MediaType: registry.ConfigMediaType,
		Digest:    digest.FromString(configContent),
		Size:      int64(len(configContent)),
	}
	if err = target.Push(ctx, *configLayer, strings.NewReader(configContent)); err != nil {
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
		return nil, fmt.Errorf("failed to push helm chart content layer: %w", err)
	}

	return &provenanceLayer, nil
}

func pushChartLayer(ctx context.Context, chart *filesystem.Blob, target oras.Target) (_ *ociImageSpecV1.Descriptor, err error) {
	// We get the reader first because Digest only returns a boolean and no error.
	// This hides errors like, "file not found" or "permission denied" on downloaded content.
	chartReader, err := chart.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to get a reader for helm chart blob: %w", err)
	}

	chartDigStr, known := chart.Digest()
	if !known {
		return nil, fmt.Errorf("unknown digest for helm chart")
	}

	chartLayer := ociImageSpecV1.Descriptor{
		MediaType: registry.ChartLayerMediaType,
		Digest:    digest.Digest(chartDigStr),
		Size:      chart.Size(),
	}
	defer func() {
		err = errors.Join(err, chartReader.Close())
	}()
	if err = target.Push(ctx, chartLayer, chartReader); err != nil {
		return nil, fmt.Errorf("failed to push helm chart content layer: %w", err)
	}

	return &chartLayer, nil
}
