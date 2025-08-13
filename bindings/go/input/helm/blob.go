package helm

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
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/registry"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"oras.land/oras-go/v2"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	v1 "ocm.software/open-component-model/bindings/go/input/helm/spec/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

var (
	ErrEmptyPath        = errors.New("helm input path must not be empty")
	ErrUnsupportedField = errors.New("unsupported input field must not be used")
)

// ReadOnlyChart contains Helm chart contents as tgz archive, some metadata and optionally a provenance file.
type ReadOnlyChart struct {
	Name      string
	Version   string
	ChartBlob *filesystem.Blob
	ProvBlob  *filesystem.Blob
}

// GetV1HelmBlob creates a ReadOnlyBlob from a v1.Helm specification.
// It reads the contents from the filesystem and packages it as an OCI artifact.
// The function returns an error if the path is empty or if there are issues reading the contents
// from the filesystem. If provided tmpDir is empty, the temporary directory will be created.
// in the system's default temp directory.
func GetV1HelmBlob(ctx context.Context, helmSpec v1.Helm, tmpDir string) (blob.ReadOnlyBlob, error) {
	if err := validateInputSpec(helmSpec); err != nil {
		return nil, fmt.Errorf("invalid helm input spec: %w", err)
	}

	chart, err := newReadOnlyChart(helmSpec.Path, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("error loading input helm chart %q: %w", helmSpec.Path, err)
	}

	b := copyChartToOCILayout(ctx, chart)

	return b, nil
}

func validateInputSpec(helmSpec v1.Helm) error {
	var err error

	if helmSpec.Path == "" {
		err = ErrEmptyPath
	}

	var unsupportedFields []string
	if helmSpec.HelmRepository != "" {
		unsupportedFields = append(unsupportedFields, "helmRepository")
	}
	if helmSpec.CACert != "" {
		unsupportedFields = append(unsupportedFields, "caCert")
	}
	if helmSpec.CACertFile != "" {
		unsupportedFields = append(unsupportedFields, "caCertFile")
	}
	if helmSpec.Version != "" {
		unsupportedFields = append(unsupportedFields, "version")
	}
	if helmSpec.Repository != "" {
		unsupportedFields = append(unsupportedFields, "repository")
	}
	if len(unsupportedFields) > 0 {
		err = errors.Join(err, ErrUnsupportedField, fmt.Errorf("%w: %s", ErrUnsupportedField, strings.Join(unsupportedFields, ", ")))
	}

	return err
}

func newReadOnlyChart(path, tmpDirBase string) (result *ReadOnlyChart, err error) {
	// Load the chart from filesystem, the path can be either a helm chart directory or a tgz file.
	// While loading the chart is also validated.
	chart, err := loader.Load(path)
	if err != nil {
		return nil, fmt.Errorf("error loading helm chart from path %q: %w", path, err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("error retrieving file information for %q: %w", path, err)
	}

	result = &ReadOnlyChart{
		Name:    chart.Name(),
		Version: chart.Metadata.Version,
	}

	if fi.IsDir() {
		// If path is a directory, we need to create a tgz archive in a temporary folder.
		tmpDir, err := os.MkdirTemp(tmpDirBase, "chartDirToTgz*")
		if err != nil {
			return nil, fmt.Errorf("error creating temporary directory")
		}

		// Save the chart as a tgz archive. If the directory is /foo, and the chart is named bar, with version 1.0.0,
		// this will generate /foo/bar-1.0.0.tgz
		// TODO: contribution to Helm to allow to write to tar in memory instead of a file.
		path, err = chartutil.Save(chart, tmpDir)
		if err != nil {
			return nil, fmt.Errorf("error saving archived chart to directory %q: %w", tmpDir, err)
		}
	}

	// Now we know that the path refers to a valid Helm chart packaged as a tgz file. Thus returning its contents.
	if result.ChartBlob, err = filesystem.GetBlobFromOSPath(path); err != nil {
		return nil, fmt.Errorf("error creating blob from file %q: %w", path, err)
	}

	provName := path + ".prov" // foo.prov
	if _, err := os.Stat(provName); err == nil {
		if result.ProvBlob, err = filesystem.GetBlobFromOSPath(provName); err != nil {
			return nil, fmt.Errorf("error creating blob from file %q: %w", path, err)
		}
	}

	return result, nil
}

// copyChartToOCILayout takes a ReadOnlyChart helper object and creates an OCI layout from it.
// Three OCI layers are expected: config, tgz contents and optionally a provenance file.
// The result is tagged with the helm chart version.
// See also: https://github.com/helm/community/blob/main/hips/hip-0006.md#2-support-for-provenance-files
func copyChartToOCILayout(ctx context.Context, chart *ReadOnlyChart) *direct.Blob {
	r, w := io.Pipe()

	go copyChartToOCILayoutAsync(ctx, chart, w)

	// TODO(ikhandamirov): replace this with a direct/unbuffered blob.
	return direct.New(r, direct.WithMediaType(layout.MediaTypeOCIImageLayoutTarGzipV1))
}

func copyChartToOCILayoutAsync(ctx context.Context, chart *ReadOnlyChart, w *io.PipeWriter) {
	// err accumulates any error from copy, gzip, or layout writing.
	var err error
	defer func() {
		_ = w.CloseWithError(err) // Always returns nil.
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
		return
	}

	if terr := target.Tag(ctx, imgDesc, chart.Version); terr != nil {
		err = fmt.Errorf("failed to tag OCI image: %w", terr)
		return
	}
}

func pushChartAndGenerateLayers(ctx context.Context, chart *ReadOnlyChart, target oras.Target) (
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
	return
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
	chartDigStr, known := chart.Digest()
	if !known {
		return nil, fmt.Errorf("unknown digest for helm chart")
	}
	chartLayer := ociImageSpecV1.Descriptor{
		MediaType: registry.ChartLayerMediaType,
		Digest:    digest.Digest(chartDigStr),
		Size:      chart.Size(),
	}
	chartReader, err := chart.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to get a reader for helm chart blob: %w", err)
	}
	defer func() {
		err = errors.Join(err, chartReader.Close())
	}()
	if err = target.Push(ctx, chartLayer, chartReader); err != nil {
		return nil, fmt.Errorf("failed to push helm chart content layer: %w", err)
	}

	return &chartLayer, nil
}
