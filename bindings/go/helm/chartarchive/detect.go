package chartarchive

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v4/pkg/registry"
	orascontent "oras.land/oras-go/v2/content"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// maxChartYAMLPeek bounds how much of a packaged chart is held in memory while looking for its
// Chart.yaml. helm package writes Chart.yaml as the first entry, so it is found within the first
// few KiB of any helm-packaged chart.
const maxChartYAMLPeek = 1 << 20

// chartMetadata is the subset of Chart.yaml (and of the helm OCI config blob) identifying a chart.
type chartMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// detect locates the packaged chart in the fetched content.
func detect(ctx context.Context, c content, id runtime.Identity) (*Chart, error) {
	if c.stream != nil {
		root := c.stream.Root()
		if root.MediaType == ocispec.MediaTypeImageManifest {
			return fromManifest(ctx, c.stream, root, id)
		}
		stream := c.stream
		c.blob = &descriptorBlob{fetch: func() (io.ReadCloser, error) { return stream.Fetch(ctx, root) }, desc: root}
		c.mediaType = root.MediaType
	}
	if strings.HasPrefix(c.mediaType, layout.MediaTypeOCIImageLayout) {
		return fromLayout(ctx, c.blob, id)
	}
	return fromArchive(c.blob, id)
}

// fromManifest resolves the chart layer of the helm chart OCI image manifest root in store.
func fromManifest(ctx context.Context, store orascontent.Fetcher, root ocispec.Descriptor, id runtime.Identity) (*Chart, error) {
	var manifest ocispec.Manifest
	if err := fetchJSON(ctx, store, root, &manifest); err != nil {
		return nil, fmt.Errorf("failed reading OCI manifest of resource %s: %w", id, err)
	}
	if manifest.Config.MediaType != registry.ConfigMediaType {
		return nil, fmt.Errorf("OCI artifact of resource %s is not a helm chart: config media type %q", id, manifest.Config.MediaType)
	}
	var meta chartMetadata
	if err := fetchJSON(ctx, store, manifest.Config, &meta); err != nil {
		return nil, fmt.Errorf("failed reading helm chart config of resource %s: %w", id, err)
	}
	if err := checkMetadata(meta, id); err != nil {
		return nil, err
	}
	for _, layer := range manifest.Layers {
		if layer.MediaType == registry.ChartLayerMediaType || layer.MediaType == registry.LegacyChartLayerMediaType {
			return &Chart{
				Name:    meta.Name,
				Version: meta.Version,
				Archive: &descriptorBlob{
					fetch: func() (io.ReadCloser, error) { return store.Fetch(ctx, layer) },
					desc:  layer,
				},
				FromOCI: true,
			}, nil
		}
	}
	return nil, fmt.Errorf("OCI artifact of resource %s has no helm chart layer (%s)", id, registry.ChartLayerMediaType)
}

// fromLayout extracts the chart layer of an OCM OCI layout, reading the layout into memory.
// Only repositories that cannot stream local resources hand out OCI artifacts this way.
func fromLayout(ctx context.Context, b blob.ReadOnlyBlob, id runtime.Identity) (*Chart, error) {
	store, err := ocitar.ReadOCILayout(ctx, b)
	if err != nil {
		return nil, fmt.Errorf("failed reading OCI layout of resource %s: %w", id, err)
	}
	defer func() { _ = store.Close() }()
	roots := store.MainArtifacts(ctx)
	if len(roots) != 1 {
		return nil, fmt.Errorf("OCI layout of resource %s must contain exactly one artifact, got %d", id, len(roots))
	}
	chart, err := fromManifest(ctx, store, roots[0], id)
	if err != nil {
		return nil, err
	}
	rc, err := chart.Archive.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed opening helm chart layer of resource %s: %w", id, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("failed reading helm chart layer of resource %s: %w", id, err)
	}
	chart.Archive = inmemory.New(bytes.NewReader(data), inmemory.WithSize(int64(len(data))))
	return chart, nil
}

// fromArchive opens b once and accepts a packaged chart (gzip) or a tar whose first .tgz
// regular file is the packaged chart (the helm downloader output: chart and provenance file).
func fromArchive(b blob.ReadOnlyBlob, id runtime.Identity) (*Chart, error) {
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed opening content of resource %s: %w", id, err)
	}
	r := bufio.NewReader(rc)
	if magic, err := r.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		size := blob.SizeUnknown
		if sized, ok := b.(blob.SizeAware); ok {
			size = sized.Size()
		}
		return fromPackaged(r, size, rc, id)
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err != nil {
			_ = rc.Close()
			return nil, fmt.Errorf("content of resource %s is neither a packaged helm chart, a tar containing one, nor a helm chart OCI artifact", id)
		}
		if hdr.Typeflag == tar.TypeReg && strings.HasSuffix(hdr.Name, ".tgz") {
			return fromPackaged(tr, hdr.Size, rc, id)
		}
	}
}

// fromPackaged reads the chart metadata from the Chart.yaml near the start of the packaged
// chart r. Only the bytes read up to Chart.yaml are held in memory; they are replayed in front
// of the rest of r, so the archive streams from a single source read. closer closes r.
func fromPackaged(r io.Reader, size int64, closer io.Closer, id runtime.Identity) (*Chart, error) {
	var consumed bytes.Buffer
	meta, found := peekChartYAML(io.TeeReader(io.LimitReader(r, maxChartYAMLPeek), &consumed))
	if !found {
		_ = closer.Close()
		return nil, fmt.Errorf("chart archive of resource %s has no Chart.yaml within its first 1 MiB", id)
	}
	if err := checkMetadata(meta, id); err != nil {
		_ = closer.Close()
		return nil, err
	}
	return &Chart{
		Name:    meta.Name,
		Version: meta.Version,
		Archive: &readerBlob{
			rc:   readCloser{Reader: io.MultiReader(&consumed, r), Closer: closer},
			size: size,
		},
	}, nil
}

func checkMetadata(meta chartMetadata, id runtime.Identity) error {
	if meta.Name == "" || meta.Version == "" {
		return fmt.Errorf("chart metadata of resource %s has no name or version", id)
	}
	return nil
}

// peekChartYAML reads the chart archive up to its top-level Chart.yaml.
func peekChartYAML(r io.Reader) (chartMetadata, bool) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return chartMetadata{}, false
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return chartMetadata{}, false
		}
		dir, file, ok := strings.Cut(strings.TrimPrefix(hdr.Name, "./"), "/")
		if !ok || dir == "" || file != "Chart.yaml" {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return chartMetadata{}, false
		}
		var meta chartMetadata
		if err := yaml.Unmarshal(data, &meta); err != nil {
			return chartMetadata{}, false
		}
		return meta, true
	}
}

func fetchJSON(ctx context.Context, store orascontent.Fetcher, desc ocispec.Descriptor, v any) error {
	rc, err := store.Fetch(ctx, desc)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	return json.NewDecoder(rc).Decode(v)
}

// descriptorBlob streams a single OCI blob on each ReadCloser call without buffering it.
type descriptorBlob struct {
	fetch func() (io.ReadCloser, error)
	desc  ocispec.Descriptor
}

var (
	_ blob.SizeAware   = (*descriptorBlob)(nil)
	_ blob.DigestAware = (*descriptorBlob)(nil)
)

func (b *descriptorBlob) ReadCloser() (io.ReadCloser, error) { return b.fetch() }

func (b *descriptorBlob) Size() int64 { return b.desc.Size }

func (b *descriptorBlob) Digest() (string, bool) { return b.desc.Digest.String(), true }
