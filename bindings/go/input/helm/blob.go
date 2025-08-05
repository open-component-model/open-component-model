package helm

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
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
	"oras.land/oras-go/v2"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
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

	b, err := copyChartToOCILayout(ctx, chart)
	if err != nil {
		return nil, fmt.Errorf("error copying helm chart to OCI layout: %w", err)
	}

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
	result.ChartBlob, err = filesystem.GetBlobFromOSPath(path)
	if err != nil {
		return nil, fmt.Errorf("error creating blob from file %q: %w", path, err)
	}

	// TODO: check for provenance file, take it along if exists.

	return result, nil
}

// copyChartToOCILayout takes a ReadOnlyChart helper object and creates an OCI layout from it.
// Three OCI layers are expected: config, tgz contents and optionally a provenance file.
// The result is tagged with the helm chart version.
// See also: https://github.com/helm/community/blob/main/hips/hip-0006.md#2-support-for-provenance-files
func copyChartToOCILayout(ctx context.Context, chart *ReadOnlyChart) (b *inmemory.Blob, err error) {
	var buf bytes.Buffer

	h := sha256.New()
	writer := io.MultiWriter(&buf, h)

	zippedBuf := gzip.NewWriter(writer)
	defer func() {
		if err != nil {
			// Clean up resources if there was an error
			zippedBuf.Close()
			buf.Reset()
		}
	}()

	target := tar.NewOCILayoutWriter(zippedBuf)
	defer func() {
		if terr := target.Close(); terr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close OCI layout writer: %w", terr))
			return
		}
		if zerr := zippedBuf.Close(); zerr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close gzip writer: %w", zerr))
			return
		}
	}()

	// Create config OCI layer.
	configContent := fmt.Sprintf(`{"name": "%s", "version": "%s"}`, chart.Name, chart.Version)
	configDigest := digest.FromString(configContent)
	configLayer := ociImageSpecV1.Descriptor{
		MediaType: registry.ConfigMediaType,
		Digest:    configDigest,
		Size:      int64(len(configContent)),
	}
	if err := target.Push(ctx, configLayer, strings.NewReader(configContent)); err != nil {
		return nil, fmt.Errorf("failed to push helm chart config layer: %w", err)
	}

	// Create content OCI layer.
	chartDigStr, known := chart.ChartBlob.Digest()
	if !known {
		return nil, fmt.Errorf("unknown digest for helm chart %q:%q", chart.Name, chart.Version)
	}
	chartLayer := ociImageSpecV1.Descriptor{
		MediaType: registry.ChartLayerMediaType,
		Digest:    digest.Digest(chartDigStr),
		Size:      chart.ChartBlob.Size(),
	}
	chartReader, err := chart.ChartBlob.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed to get a reader for helm chart blob %q:%q: %w", chart.Name, chart.Version, err)
	}
	if err = target.Push(ctx, chartLayer, chartReader); err != nil {
		return nil, fmt.Errorf("failed to push helm chart content layer: %w", err)
	}

	// TODO: create and push provenance OCI layer.

	// Create OCI image manifest.
	imgDesc, err := oras.PackManifest(ctx, target, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{
		ConfigDescriptor: &configLayer,
		Layers:           []ociImageSpecV1.Descriptor{chartLayer}, // TODO: add provenance layer.
	})

	if err := target.Tag(ctx, imgDesc, chart.Version); err != nil {
		return nil, fmt.Errorf("failed to tag base: %w", err)
	}

	// Now close prematurely so that the buf is fully filled before we set things like size and digest.
	if err := errors.Join(target.Close(), zippedBuf.Close()); err != nil {
		return nil, fmt.Errorf("failed to close writers: %w", err)
	}

	// Explicitly close the readers.
	if err := chartReader.Close(); err != nil { // TODO: close provenance reader.
		return nil, fmt.Errorf("failed to close readers: %w", err)
	}

	// TODO(ikhandamirov): replace this with a direct/unbuffered blob.
	b = inmemory.New(&buf,
		inmemory.WithSize(int64(buf.Len())),
		inmemory.WithDigest(digest.NewDigest(digest.SHA256, h).String()),
		inmemory.WithMediaType(layout.MediaTypeOCIImageLayoutTarGzipV1),
	)

	return b, nil
}
