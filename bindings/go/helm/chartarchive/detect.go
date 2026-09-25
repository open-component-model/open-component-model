package chartarchive

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v4/pkg/registry"
	orascontent "oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// detect locates the packaged chart in the fetched content. The chart itself is not parsed:
// the target (e.g. an Artifactory Helm repository) reads and validates it.
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
	for _, layer := range manifest.Layers {
		if layer.MediaType == registry.ChartLayerMediaType || layer.MediaType == registry.LegacyChartLayerMediaType {
			return &Chart{
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
	layer := chart.Archive.(*descriptorBlob)
	rc, err := layer.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed opening helm chart layer of resource %s: %w", id, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("failed reading helm chart layer of resource %s: %w", id, err)
	}
	// The in-memory copy keeps the layer digest, so the digest stays known up front.
	chart.Archive = &descriptorBlob{
		fetch: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil },
		desc:  layer.desc,
	}
	return chart, nil
}

// fromArchive opens b once and accepts a packaged chart (gzip) as is, or a tar whose first .tgz
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
		return &Chart{Archive: &readerBlob{rc: readCloser{Reader: r, Closer: rc}, size: size}}, nil
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err != nil {
			_ = rc.Close()
			return nil, fmt.Errorf("content of resource %s is neither a packaged helm chart, a tar containing one, nor a helm chart OCI artifact", id)
		}
		if hdr.Typeflag == tar.TypeReg && strings.HasSuffix(hdr.Name, ".tgz") {
			return &Chart{Archive: &readerBlob{rc: readCloser{Reader: tr, Closer: rc}, size: hdr.Size}}, nil
		}
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
