package repositoryupload

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v4/pkg/registry"
	orascontent "oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// LocateChart returns the packaged Helm chart (.tgz) in content: content itself when it is gzip, the
// first .tgz of a tar (helm downloader output), or the chart layer of a Helm chart OCI layout.
// fromOCI reports that the chart came from an OCI artifact, so the source digest does not describe it.
// The chart is located, not parsed: the target repository reads and validates it.
func LocateChart(ctx context.Context, content blob.ReadOnlyBlob, mediaType string, id runtime.Identity) (chart blob.ReadOnlyBlob, fromOCI bool, err error) {
	if isOCILayout(mediaType) {
		chart, err := fromLayout(ctx, content, id)
		return chart, true, err
	}
	chart, err = fromArchive(content, id)
	return chart, false, err
}

// isOCILayout reports whether mediaType is that of an OCM OCI layout, the form OCI artifacts are
// downloaded in.
func isOCILayout(mediaType string) bool {
	return strings.HasPrefix(mediaType, layout.MediaTypeOCIImageLayout)
}

// ociHelmChart reports whether src is a Helm chart stored in an OCI registry. Its digest is that
// of the chart manifest (see helm/digest), not of the downloaded chart.
func ociHelmChart(src *descriptor.Resource) bool {
	if src.Access == nil || !helmaccess.Scheme.IsRegistered(src.Access.GetType()) {
		return false
	}
	var access helmaccessv1.Helm
	if err := helmaccess.Scheme.Convert(src.Access, &access); err != nil {
		return false
	}
	ref, err := access.ChartReference()
	return err == nil && strings.HasPrefix(ref, "oci://")
}

// fromManifest resolves the chart layer of the helm chart OCI image manifest root in store.
func fromManifest(ctx context.Context, store orascontent.Fetcher, root ocispec.Descriptor, id runtime.Identity) (ocispec.Descriptor, error) {
	var manifest ocispec.Manifest
	if err := fetchJSON(ctx, store, root, &manifest); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed reading OCI manifest of resource %s: %w", id, err)
	}
	if manifest.Config.MediaType != registry.ConfigMediaType {
		return ocispec.Descriptor{}, fmt.Errorf("OCI artifact of resource %s is not a helm chart: config media type %q", id, manifest.Config.MediaType)
	}
	for _, layer := range manifest.Layers {
		if layer.MediaType == registry.ChartLayerMediaType || layer.MediaType == registry.LegacyChartLayerMediaType {
			return layer, nil
		}
	}
	return ocispec.Descriptor{}, fmt.Errorf("OCI artifact of resource %s has no helm chart layer (%s)", id, registry.ChartLayerMediaType)
}

// fromLayout extracts the chart layer of an OCM OCI layout, reading the layer into memory.
func fromLayout(ctx context.Context, b blob.ReadOnlyBlob, id runtime.Identity) (blob.ReadOnlyBlob, error) {
	store, err := ocitar.ReadOCILayout(ctx, b)
	if err != nil {
		return nil, fmt.Errorf("failed reading OCI layout of resource %s: %w", id, err)
	}
	defer func() { _ = store.Close() }()
	roots := store.MainArtifacts(ctx)
	if len(roots) != 1 {
		return nil, fmt.Errorf("OCI layout of resource %s must contain exactly one artifact, got %d", id, len(roots))
	}
	layer, err := fromManifest(ctx, store, roots[0], id)
	if err != nil {
		return nil, err
	}
	rc, err := store.Fetch(ctx, layer)
	if err != nil {
		return nil, fmt.Errorf("failed opening helm chart layer of resource %s: %w", id, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("failed reading helm chart layer of resource %s: %w", id, err)
	}
	// The in-memory copy keeps the layer digest, so the digest stays known up front.
	return &descriptorBlob{data: data, desc: layer}, nil
}

// fromArchive opens b once and accepts a packaged chart (gzip) as is, or a tar whose first .tgz
// regular file is the packaged chart (the helm downloader output: chart and provenance file).
func fromArchive(b blob.ReadOnlyBlob, id runtime.Identity) (blob.ReadOnlyBlob, error) {
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
		return &readerBlob{rc: readCloser{Reader: r, Closer: rc}, size: size}, nil
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err != nil {
			_ = rc.Close()
			return nil, fmt.Errorf("content of resource %s is neither a packaged helm chart, a tar containing one, nor a helm chart OCI artifact", id)
		}
		if hdr.Typeflag == tar.TypeReg && strings.HasSuffix(hdr.Name, ".tgz") {
			return &readerBlob{rc: readCloser{Reader: tr, Closer: rc}, size: hdr.Size}, nil
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

// descriptorBlob is an in-memory OCI blob that keeps the size and digest of its descriptor.
type descriptorBlob struct {
	data []byte
	desc ocispec.Descriptor
}

var (
	_ blob.SizeAware   = (*descriptorBlob)(nil)
	_ blob.DigestAware = (*descriptorBlob)(nil)
)

func (b *descriptorBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func (b *descriptorBlob) Size() int64 { return b.desc.Size }

func (b *descriptorBlob) Digest() (string, bool) { return b.desc.Digest.String(), true }

// readerBlob hands out an already opened stream exactly once.
type readerBlob struct {
	rc   io.ReadCloser
	size int64
	used bool
}

var _ blob.SizeAware = (*readerBlob)(nil)

func (b *readerBlob) ReadCloser() (io.ReadCloser, error) {
	if b.used {
		return nil, errors.New("chart stream can only be read once")
	}
	b.used = true
	return b.rc, nil
}

func (b *readerBlob) Size() int64 { return b.size }

// Close closes the stream if it was never handed out; once handed out, the reader owns it.
func (b *readerBlob) Close() error {
	if b.used {
		return nil
	}
	b.used = true
	return b.rc.Close()
}

type readCloser struct {
	io.Reader
	io.Closer
}
