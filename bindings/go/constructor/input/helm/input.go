package helm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/registry"
	"k8s.io/apimachinery/pkg/util/json"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor/input"
	"ocm.software/open-component-model/bindings/go/constructor/input/helm/spec/v1"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	ocilayout "ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ input.Method = &Method{}

type Method struct{ Scheme *runtime.Scheme }

func (i *Method) ProcessResource(ctx context.Context, resource *spec.Resource) (data blob.ReadOnlyBlob, err error) {
	return i.process(ctx, resource)
}

func (i *Method) process(ctx context.Context, input *spec.Resource) (blob.ReadOnlyBlob, error) {
	helm := v1.Helm{}
	if err := i.Scheme.Convert(input.Input, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource input helm: %w", err)
	}

	var buf bytes.Buffer
	layout := tar.NewOCILayoutWriter(&buf)
	defer func() {
		_ = layout.Close()
	}()

	var helmChart *chart.Chart
	if helm.Path != "" {
		stat, err := os.Stat(helm.Path)
		if err != nil {
			return nil, fmt.Errorf("error checking helm chart source path %q: %w", helm.Path, err)
		}
		if stat.IsDir() {
			helmChart, err = loader.LoadDir(helm.Path)
		} else {
			helmChart, err = loader.LoadFile(helm.Path)
		}
		if err != nil {
			return nil, fmt.Errorf("error loading helm chart from path %q: %w", helm.Path, err)
		}
	}

	if helmChart == nil {
		return nil, fmt.Errorf("no valid helm chart found at path %q", helm.Path)
	}

	meta := helmChart.Metadata

	// Set the version of the resource to the version of the helm chart
	input.Version = meta.Version

	metaRaw, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("error encoding helm chart metadata: %w", err)
	}
	metaDesc := content.NewDescriptorFromBytes(registry.ConfigMediaType, metaRaw)
	if err := layout.Push(ctx, metaDesc, bytes.NewReader(metaRaw)); err != nil {
		return nil, fmt.Errorf("error writing helm config (%q) to layout: %w", registry.ConfigMediaType, err)
	}

	tmp, err := os.MkdirTemp("", "helmchart-")
	if err != nil {
		return nil, fmt.Errorf("cannot create temporary directory for helm chart: %w", err)
	}

	archive, err := chartutil.Save(helmChart, tmp)
	if err != nil {
		return nil, fmt.Errorf("error saving helm chart to temporary directory: %w", err)
	}
	if archive, err = filepath.Rel(tmp, archive); err != nil {
		return nil, fmt.Errorf("error getting relative path of helm chart: %w", err)
	}

	fs, err := filesystem.NewFS(tmp, os.O_RDONLY)
	if err != nil {
		return nil, fmt.Errorf("error creating filesystem from temporary directory: %w", err)
	}

	chartBlob := filesystem.NewFileBlob(fs, archive)
	chartDigest, ok := chartBlob.Digest()
	if !ok {
		return nil, fmt.Errorf("could not get digest of helm chart")
	}
	chartDesc := ociImageSpecV1.Descriptor{
		MediaType: registry.ChartLayerMediaType,
		Digest:    digest.Digest(chartDigest),
		Size:      chartBlob.Size(),
	}
	chartData, err := chartBlob.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("error reading helm chart blob: %w", err)
	}
	defer func() {
		_ = chartData.Close()
	}()
	if err := layout.Push(ctx, chartDesc, chartData); err != nil {
		return nil, fmt.Errorf("error writing helm chart (%q) to layout: %w", registry.ChartLayerMediaType, err)
	}

	if _, err := oras.PackManifest(ctx, layout, oras.PackManifestVersion1_1, "", oras.PackManifestOptions{
		Layers:           []ociImageSpecV1.Descriptor{chartDesc},
		ConfigDescriptor: &metaDesc,
	}); err != nil {
		return nil, fmt.Errorf("error packing helm chart: %w", err)
	}

	b := blob.NewDirectReadOnlyBlob(&buf)
	b.SetMediaType(ocilayout.MediaTypeOCIImageLayoutTarGzipV1)

	return b, nil
}
