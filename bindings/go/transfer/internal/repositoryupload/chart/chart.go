// Package chart locates the packaged Helm chart in the content of a resource and describes charts
// published to Helm repositories.
package chart

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

// Locate returns the packaged Helm chart (.tgz) in content: content itself when it is gzip, the
// first .tgz of a tar (helm downloader output), or the chart layer of a Helm chart OCI layout.
// fromOCI reports that the chart came from an OCI artifact, so the source digest does not describe it.
// The chart is located, not parsed: the target repository reads and validates it.
func Locate(ctx context.Context, content blob.ReadOnlyBlob, mediaType string, id runtime.Identity) (chart blob.ReadOnlyBlob, fromOCI bool, err error) {
	if layout.IsOCILayoutMediaType(mediaType) {
		chart, err := fromLayout(ctx, content, id)
		return chart, true, err
	}
	chart, err = fromArchive(content, id)
	return chart, false, err
}

// FromOCIRegistry reports whether src is a Helm chart stored in an OCI registry. Its digest is that
// of the chart manifest (see helm/digest), not of the downloaded chart.
func FromOCIRegistry(src *descriptor.Resource) bool {
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

// fromArchive accepts a packaged chart (gzip) as is, or a tar whose first .tgz regular file
// is the packaged chart (the helm downloader output: chart and provenance file). b is read
// once here to find the chart and again on every read of the returned blob, which holds
// nothing open in between.
func fromArchive(b blob.ReadOnlyBlob, id runtime.Identity) (blob.ReadOnlyBlob, error) {
	rc, size, err := openChart(b)
	if err != nil {
		return nil, fmt.Errorf("content of resource %s: %w", id, err)
	}
	// Only the start of the content was read to find the chart; a verifying source rejects
	// that partial read on Close, which is expected. Every read of the returned blob is
	// verified in full.
	_ = rc.Close()
	return &chartBlob{source: b, size: size}, nil
}

// openChart opens b and returns a reader of the packaged chart in it and its size.
func openChart(b blob.ReadOnlyBlob) (io.ReadCloser, int64, error) {
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, 0, fmt.Errorf("failed opening content: %w", err)
	}
	r := bufio.NewReader(rc)
	if magic, err := r.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		size := blob.SizeUnknown
		if sized, ok := b.(blob.SizeAware); ok {
			size = sized.Size()
		}
		return readCloser{Reader: r, Closer: rc}, size, nil
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err != nil {
			_ = rc.Close()
			return nil, 0, errors.New("neither a packaged helm chart, a tar containing one, nor a helm chart OCI artifact")
		}
		if hdr.Typeflag == tar.TypeReg && strings.HasSuffix(hdr.Name, ".tgz") {
			return &tarChart{tr: tr, rest: r, Closer: rc}, hdr.Size, nil
		}
	}
}

// tarChart reads the chart entry of a tar. A source verifying its content checks it when
// closed, which fails unless it was read to its end, so once the chart has been read
// completely Close reads the rest of the tar before closing the source.
type tarChart struct {
	tr   *tar.Reader
	rest io.Reader
	io.Closer
	done bool
}

func (c *tarChart) Read(p []byte) (int, error) {
	n, err := c.tr.Read(p)
	if errors.Is(err, io.EOF) {
		c.done = true
	}
	return n, err
}

func (c *tarChart) Close() error {
	var drainErr error
	if c.done {
		_, drainErr = io.Copy(io.Discard, c.rest)
	}
	return errors.Join(drainErr, c.Closer.Close())
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

// chartBlob is the packaged chart in source, located again on every read.
type chartBlob struct {
	source blob.ReadOnlyBlob
	size   int64
}

var _ blob.SizeAware = (*chartBlob)(nil)

func (b *chartBlob) ReadCloser() (io.ReadCloser, error) {
	rc, _, err := openChart(b.source)
	return rc, err
}

func (b *chartBlob) Size() int64 { return b.size }

type readCloser struct {
	io.Reader
	io.Closer
}

// Access returns the Helm/v1 access of chart name:version in helmRepo. It rejects a name
// containing ":" or "/" and a version containing "/", which server recorded for the chart at url.
func Access(server, helmRepo, name, version, url string) (runtime.Typed, error) {
	if strings.ContainsAny(name, ":/") || strings.Contains(version, "/") {
		return nil, fmt.Errorf("%s recorded an invalid chart name %q or version %q for %s", server, name, version, url)
	}
	return &helmaccessv1.Helm{
		Type:           runtime.NewVersionedType(helmaccessv1.Type, helmaccessv1.Version),
		HelmRepository: helmRepo,
		HelmChart:      name + ":" + version,
	}, nil
}
