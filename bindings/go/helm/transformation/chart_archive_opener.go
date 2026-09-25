package transformation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2/content"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmblob "ocm.software/open-component-model/bindings/go/helm/blob"
	"ocm.software/open-component-model/bindings/go/helm/internal/download"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ChartArchiveOpener is the HTTPStreamingSpec.Opener name under which ChartArchiveSource.Open is registered.
const ChartArchiveOpener = "HelmChartArchive"

// maxChartYAMLPeek bounds how much of a packaged chart is held in memory while looking for its
// Chart.yaml. helm package writes Chart.yaml as the first entry, so it is found within the first
// few KiB of any helm-packaged chart.
const maxChartYAMLPeek = 1 << 20

// chartArchiveMediaTypes are local blob media types whose content already is the packaged chart (.tgz).
var chartArchiveMediaTypes = []string{
	registry.ChartLayerMediaType,
	registry.LegacyChartLayerMediaType,
	"application/gzip",
	"application/x-gzip",
	"application/x-tgz",
}

// ChartArchiveSource opens the packaged Helm chart archive (.tgz) of a resource so it can be
// streamed to a classic Helm repository such as JFrog Artifactory. Wherever possible the chart
// streams straight from its source without being written to disk or held in memory.
type ChartArchiveSource struct {
	// ResourceRepository downloads non-OCI remote sources, and Helm/v1 charts that cannot be
	// streamed (see download.ErrNotStreamable).
	ResourceRepository repository.ResourceRepository
	// OCIRepository streams the chart layer of OCIImage sources and oci:// Helm charts.
	OCIRepository ocistream.ResourceRepository
	// HTTPConfig configures the client streaming charts from HTTP/S Helm repositories.
	HTTPConfig *httpv1alpha1.Config
}

// LocalSource locates a local blob resource in its source component version.
type LocalSource struct {
	Repository repository.ComponentVersionRepository
	Component  string
	Version    string
}

// ChartRequest describes the source to open.
type ChartRequest struct {
	// Resource is the source resource with its original access.
	Resource *descriptor.Resource
	// Target is the resource as it will be published. When its access is a Helm access, the
	// chart name and version it publishes are checked against the chart's own metadata.
	Target *descriptor.Resource
	// Credentials are the resolved source credentials (remote sources only).
	Credentials runtime.Typed
	// Local is set for a local blob resource.
	Local *LocalSource
}

// OpenedChart is the chart archive to upload.
type OpenedChart struct {
	Archive blob.ReadOnlyBlob
	// Derived reports that Archive is not the byte representation the source digest
	// describes (e.g. the chart layer of an OCI artifact).
	Derived bool
}

// localResourceStreamer is implemented by OCI and CTF component version repositories.
type localResourceStreamer interface {
	GetLocalResourceStream(ctx context.Context, component, version string, identity runtime.Identity) (ocistream.ResourceStream, *descriptor.Resource, error)
}

// chartMetadata is the subset of Chart.yaml (and of the helm OCI config blob) identifying a chart.
type chartMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Open returns the chart archive of the source:
//   - Helm access (HTTP/S): the chart .tgz streamed from the URL listed in the repository index.
//   - Helm access (oci://) and OCIImage access: the helm chart layer, streamed from the registry.
//   - LocalBlob access: streamed from the source component version; a packaged chart as is,
//     an OCI artifact (as stored by the helm input) via its chart layer.
//   - any other access: the downloaded bytes unchanged, which must already be the chart .tgz.
//
// The chart name and version published by req.Target must match the chart's own metadata,
// because Helm repositories index charts by Chart.yaml and a mismatching published reference
// could not be pulled. It is read from the OCI config blob, or from the Chart.yaml at the start
// of a packaged chart while the stream is opened.
func (s *ChartArchiveSource) Open(ctx context.Context, req ChartRequest) (OpenedChart, error) {
	if req.Resource == nil || req.Resource.Access == nil {
		return OpenedChart{}, errors.New("resource access is required")
	}
	if req.Local != nil {
		return s.openLocalBlob(ctx, req)
	}
	typ := req.Resource.Access.GetType()
	switch {
	case helmaccess.Scheme.IsRegistered(typ):
		return s.openHelm(ctx, req)
	case isOCIImage(typ):
		stream, err := s.OCIRepository.DownloadResourceStream(ctx, req.Resource, req.Credentials)
		if err != nil {
			return OpenedChart{}, fmt.Errorf("failed resolving OCI artifact of resource %s: %w", req.Resource.ToIdentity(), err)
		}
		layer, err := chartLayer(ctx, stream, stream.Root(), req)
		if err != nil {
			return OpenedChart{}, err
		}
		return OpenedChart{Archive: layer, Derived: true}, nil
	default:
		src, err := s.ResourceRepository.DownloadResource(ctx, req.Resource, req.Credentials)
		if err != nil {
			return OpenedChart{}, err
		}
		checked, err := checkChart(src, req.Target)
		return OpenedChart{Archive: checked}, err
	}
}

func (s *ChartArchiveSource) openHelm(ctx context.Context, req ChartRequest) (OpenedChart, error) {
	var access helmaccessv1.Helm
	if err := helmaccess.Scheme.Convert(req.Resource.Access, &access); err != nil {
		return OpenedChart{}, fmt.Errorf("error converting access to helm spec: %w", err)
	}
	ref, err := access.ChartReference()
	if err != nil {
		return OpenedChart{}, fmt.Errorf("error constructing chart reference: %w", err)
	}

	if ociRef, ok := strings.CutPrefix(ref, "oci://"); ok {
		return s.openHelmOCI(ctx, req, ociRef)
	}

	opts := []download.Option{download.WithHTTPConfig(s.HTTPConfig)}
	if req.Credentials != nil {
		creds, err := helmcredsv1.ConvertToHelmHTTPCredentials(req.Credentials)
		if err != nil {
			return OpenedChart{}, fmt.Errorf("error converting credentials: %w", err)
		}
		opts = append(opts, download.WithCredentials(creds))
	}
	rc, size, err := download.OpenHTTPChart(ctx, ref, opts...)
	switch {
	case errors.Is(err, download.ErrNotStreamable):
		// Client certificates, custom CAs or provenance verification need the helm downloader,
		// which works on files.
		slog.DebugContext(ctx, "helm chart cannot be streamed, downloading it", "chart", ref)
		return s.downloadHelm(ctx, req)
	case err != nil:
		return OpenedChart{}, fmt.Errorf("failed streaming helm chart: %w", err)
	}
	if size < 0 {
		size = blob.SizeUnknown
	}
	checked, err := checkChart(&readerBlob{rc: rc, size: size}, req.Target)
	return OpenedChart{Archive: checked}, err
}

// openHelmOCI streams the chart layer of an oci:// helm chart. The layer is the chart .tgz the
// helm downloader would produce, so the source digest still describes it.
func (s *ChartArchiveSource) openHelmOCI(ctx context.Context, req ChartRequest, ociRef string) (OpenedChart, error) {
	ociResource := req.Resource.DeepCopy()
	ociResource.Access = &ociaccessv1.OCIImage{
		Type:           runtime.NewVersionedType(ociaccessv1.OCIImageType, ociaccessv1.Version),
		ImageReference: ociRef,
	}
	var creds runtime.Typed
	if req.Credentials != nil {
		ociCreds, err := helmcredsv1.ConvertToOCICredentials(req.Credentials)
		if err != nil {
			return OpenedChart{}, fmt.Errorf("error converting credentials: %w", err)
		}
		creds = ociCreds
	}
	stream, err := s.OCIRepository.DownloadResourceStream(ctx, ociResource, creds)
	if err != nil {
		return OpenedChart{}, fmt.Errorf("failed resolving OCI helm chart %s: %w", ociRef, err)
	}
	layer, err := chartLayer(ctx, stream, stream.Root(), req)
	if err != nil {
		return OpenedChart{}, err
	}
	return OpenedChart{Archive: layer}, nil
}

// downloadHelm uses the helm downloader, which buffers the chart and its provenance file.
func (s *ChartArchiveSource) downloadHelm(ctx context.Context, req ChartRequest) (OpenedChart, error) {
	src, err := s.ResourceRepository.DownloadResource(ctx, req.Resource, req.Credentials)
	if err != nil {
		return OpenedChart{}, fmt.Errorf("failed downloading helm chart: %w", err)
	}
	chart, err := helmblob.NewChartBlob(src).ChartArchive()
	if err != nil {
		return OpenedChart{}, fmt.Errorf("failed extracting chart archive from helm download: %w", err)
	}
	checked, err := checkChart(chart, req.Target)
	return OpenedChart{Archive: checked}, err
}

func (s *ChartArchiveSource) openLocalBlob(ctx context.Context, req ChartRequest) (OpenedChart, error) {
	local, id := req.Local, req.Resource.ToIdentity()
	descriptorMediaType := localBlobMediaType(req.Resource.Access)

	if streamer, ok := local.Repository.(localResourceStreamer); ok {
		stream, _, err := streamer.GetLocalResourceStream(ctx, local.Component, local.Version, id)
		if err != nil {
			return OpenedChart{}, fmt.Errorf("failed resolving local blob of resource %s: %w", id, err)
		}
		root := stream.Root()
		switch {
		case root.MediaType == ocispec.MediaTypeImageManifest:
			layer, err := chartLayer(ctx, stream, root, req)
			if err != nil {
				return OpenedChart{}, err
			}
			return OpenedChart{Archive: layer, Derived: true}, nil
		case isChartArchiveMediaType(baseMediaType(descriptorMediaType)) || isChartArchiveMediaType(baseMediaType(root.MediaType)):
			checked, err := checkChart(&descriptorBlob{fetch: func() (io.ReadCloser, error) { return stream.Fetch(ctx, root) }, desc: root}, req.Target)
			return OpenedChart{Archive: checked}, err
		default:
			return OpenedChart{}, unsupportedLocalBlob(id, descriptorMediaType)
		}
	}

	// Repositories without streaming access hand out local OCI artifacts as OCM OCI layouts,
	// whose chart layer can only be located after reading the layout.
	slog.DebugContext(ctx, "component version repository cannot stream local resources, reading the local blob", "resource", id)
	b, _, err := local.Repository.GetLocalResource(ctx, local.Component, local.Version, id)
	if err != nil {
		return OpenedChart{}, fmt.Errorf("failed getting local blob of resource %s: %w", id, err)
	}
	mediaType := descriptorMediaType
	if mt, ok := b.(blob.MediaTypeAware); ok {
		if m, known := mt.MediaType(); known && m != "" && m != "application/octet-stream" {
			mediaType = m
		}
	}
	switch base := baseMediaType(mediaType); {
	case isChartArchiveMediaType(base):
		checked, err := checkChart(b, req.Target)
		return OpenedChart{Archive: checked}, err
	case strings.HasPrefix(base, layout.MediaTypeOCIImageLayoutV1):
		return chartFromLayout(ctx, b, req)
	default:
		return OpenedChart{}, unsupportedLocalBlob(id, mediaType)
	}
}

func unsupportedLocalBlob(id runtime.Identity, mediaType string) error {
	return fmt.Errorf("local blob of resource %s has media type %q, which is neither a packaged helm chart (%s) nor an OCI artifact",
		id, mediaType, strings.Join(chartArchiveMediaTypes, ", "))
}

// chartFromLayout extracts the chart layer of an OCM OCI layout, reading the layout into memory.
func chartFromLayout(ctx context.Context, b blob.ReadOnlyBlob, req ChartRequest) (OpenedChart, error) {
	id := req.Resource.ToIdentity()
	store, err := ocitar.ReadOCILayout(ctx, b)
	if err != nil {
		return OpenedChart{}, fmt.Errorf("failed reading OCI layout of resource %s: %w", id, err)
	}
	defer func() { _ = store.Close() }()
	roots := store.MainArtifacts(ctx)
	if len(roots) != 1 {
		return OpenedChart{}, fmt.Errorf("OCI layout of resource %s must contain exactly one artifact, got %d", id, len(roots))
	}
	layer, err := chartLayer(ctx, store, roots[0], req)
	if err != nil {
		return OpenedChart{}, err
	}
	rc, err := layer.ReadCloser()
	if err != nil {
		return OpenedChart{}, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		return OpenedChart{}, fmt.Errorf("failed reading helm chart layer of resource %s: %w", id, err)
	}
	return OpenedChart{Archive: inmemory.New(bytes.NewReader(data), inmemory.WithSize(int64(len(data)))), Derived: true}, nil
}

// chartLayer resolves the helm chart layer of the OCI image manifest root in store, after
// checking the chart metadata from the helm config blob against req.Target.
func chartLayer(ctx context.Context, store content.Fetcher, root ocispec.Descriptor, req ChartRequest) (*descriptorBlob, error) {
	id := req.Resource.ToIdentity()
	if root.MediaType != ocispec.MediaTypeImageManifest {
		return nil, fmt.Errorf("helm chart of resource %s must be an OCI image manifest, got %q", id, root.MediaType)
	}
	var manifest ocispec.Manifest
	if err := fetchJSON(ctx, store, root, &manifest); err != nil {
		return nil, fmt.Errorf("failed reading OCI manifest of resource %s: %w", id, err)
	}
	if manifest.Config.MediaType == registry.ConfigMediaType {
		var meta chartMetadata
		if err := fetchJSON(ctx, store, manifest.Config, &meta); err != nil {
			return nil, fmt.Errorf("failed reading helm chart config of resource %s: %w", id, err)
		}
		if name, version, ok := publishedChart(req.Target); ok {
			if err := compareChart(meta, name, version); err != nil {
				return nil, err
			}
		}
	}
	for _, layer := range manifest.Layers {
		if layer.MediaType == registry.ChartLayerMediaType || layer.MediaType == registry.LegacyChartLayerMediaType {
			return &descriptorBlob{
				fetch: func() (io.ReadCloser, error) { return store.Fetch(ctx, layer) },
				desc:  layer,
			}, nil
		}
	}
	return nil, fmt.Errorf("OCI artifact of resource %s has no helm chart layer (%s)", id, registry.ChartLayerMediaType)
}

func fetchJSON(ctx context.Context, store content.Fetcher, desc ocispec.Descriptor, v any) error {
	rc, err := store.Fetch(ctx, desc)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	return json.NewDecoder(rc).Decode(v)
}

// checkChart opens the packaged chart b and compares the Chart.yaml at its start with the chart
// name and version target publishes. Only the bytes read up to Chart.yaml are held in memory;
// they are replayed in front of the rest of the stream, so the returned blob yields the full
// archive from a single source read. A chart whose Chart.yaml is not within the first
// maxChartYAMLPeek bytes is not checked.
func checkChart(b blob.ReadOnlyBlob, target *descriptor.Resource) (blob.ReadOnlyBlob, error) {
	name, version, ok := publishedChart(target)
	if !ok {
		return b, nil
	}
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, fmt.Errorf("failed opening chart archive: %w", err)
	}
	var consumed bytes.Buffer
	meta, found := peekChartYAML(io.TeeReader(io.LimitReader(rc, maxChartYAMLPeek), &consumed))
	if found {
		if err := compareChart(meta, name, version); err != nil {
			_ = rc.Close()
			return nil, err
		}
	} else {
		slog.Debug("no Chart.yaml found at the start of the chart archive, skipping the chart name and version check")
	}
	size := blob.SizeUnknown
	if sized, ok := b.(blob.SizeAware); ok {
		size = sized.Size()
	}
	return &readerBlob{
		rc:   readCloser{Reader: io.MultiReader(&consumed, rc), Closer: rc},
		size: size,
	}, nil
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

func compareChart(meta chartMetadata, name, version string) error {
	if meta.Name != name || meta.Version != version {
		return fmt.Errorf("chart is %s:%s according to its Chart.yaml, but it would be published as %s:%s; "+
			"Helm repositories index charts by Chart.yaml, so set chartName/chartVersion to match", meta.Name, meta.Version, name, version)
	}
	return nil
}

// publishedChart returns the chart name and version target publishes, if its access is a Helm access.
func publishedChart(target *descriptor.Resource) (name, version string, ok bool) {
	if target == nil || target.Access == nil || !helmaccess.Scheme.IsRegistered(target.Access.GetType()) {
		return "", "", false
	}
	var access helmaccessv1.Helm
	if err := helmaccess.Scheme.Convert(target.Access, &access); err != nil {
		return "", "", false
	}
	return access.GetChartName(), access.GetVersion(), true
}

func localBlobMediaType(access runtime.Typed) string {
	var lb descriptorv2.LocalBlob
	if err := descriptorv2.Scheme.Convert(access, &lb); err != nil {
		return ""
	}
	return lb.MediaType
}

func baseMediaType(mediaType string) string {
	return strings.TrimSpace(strings.SplitN(mediaType, ";", 2)[0])
}

func isChartArchiveMediaType(mediaType string) bool {
	for _, mt := range chartArchiveMediaTypes {
		if mediaType == mt {
			return true
		}
	}
	return false
}

func isOCIImage(typ runtime.Type) bool {
	obj, err := ociaccess.Scheme.NewObject(typ)
	if err != nil {
		return false
	}
	_, ok := obj.(*ociaccessv1.OCIImage)
	return ok
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

type readCloser struct {
	io.Reader
	io.Closer
}
