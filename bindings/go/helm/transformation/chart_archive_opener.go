package transformation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmblob "ocm.software/open-component-model/bindings/go/helm/blob"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
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

// chartArchiveMediaTypes are local blob media types whose content already is the packaged chart (.tgz).
var chartArchiveMediaTypes = []string{
	registry.ChartLayerMediaType,
	registry.LegacyChartLayerMediaType,
	"application/gzip",
	"application/x-gzip",
	"application/x-tgz",
}

// ChartArchiveSource opens the packaged Helm chart archive (.tgz) of a resource so it can be
// streamed to a classic Helm repository such as JFrog Artifactory.
type ChartArchiveSource struct {
	// ResourceRepository downloads Helm/v1 and all non-OCI remote sources.
	ResourceRepository repository.ResourceRepository
	// OCIRepository streams the chart layer of OCIImage sources directly from the registry.
	OCIRepository ocistream.ResourceRepository
}

// ChartRequest describes the source to open.
type ChartRequest struct {
	// Resource is the source resource with its original access.
	Resource *descriptor.Resource
	// Target is the resource as it will be published. When its access is a Helm access, the
	// chart name and version it publishes are checked against the chart's own metadata.
	Target *descriptor.Resource
	// Credentials are the resolved source credentials.
	Credentials runtime.Typed
	// Buffered is the already fetched content of a local blob source; nil for remote sources.
	Buffered blob.ReadOnlyBlob
}

// OpenedChart is the chart archive to upload.
type OpenedChart struct {
	Archive blob.ReadOnlyBlob
	// Derived reports that Archive is not the byte representation the source digest
	// describes (e.g. the chart layer of an OCI artifact or OCI layout).
	Derived bool
}

// chartMetadata is the subset of Chart.yaml (and of the helm OCI config blob) identifying a chart.
type chartMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Open returns the chart archive of the source:
//   - Helm access: the chart .tgz extracted from the helm download (the provenance file is dropped).
//   - OCIImage access: the helm chart layer, streamed lazily from the registry.
//   - LocalBlob access: chosen by the local blob media type; a packaged chart is uploaded as is,
//     an OCM OCI layout (as produced by the helm input) yields its chart layer.
//   - any other access: the downloaded bytes unchanged, which must already be the chart .tgz.
//
// Where the chart metadata is available without an extra download (all but the last case), it
// must match the chart name and version published by req.Target, because Helm repositories
// index charts by their Chart.yaml and a mismatching published reference could not be pulled.
func (s *ChartArchiveSource) Open(ctx context.Context, req ChartRequest) (OpenedChart, error) {
	if req.Resource == nil || req.Resource.Access == nil {
		return OpenedChart{}, errors.New("resource access is required")
	}
	if req.Buffered != nil {
		return s.openLocalBlob(ctx, req)
	}
	typ := req.Resource.Access.GetType()
	switch {
	case helmaccess.Scheme.IsRegistered(typ):
		src, err := s.ResourceRepository.DownloadResource(ctx, req.Resource, req.Credentials)
		if err != nil {
			return OpenedChart{}, fmt.Errorf("failed downloading helm chart: %w", err)
		}
		chart, err := helmblob.NewChartBlob(src).ChartArchive()
		if err != nil {
			return OpenedChart{}, fmt.Errorf("failed extracting chart archive from helm download: %w", err)
		}
		if err := verifyArchive(chart, req.Target); err != nil {
			return OpenedChart{}, err
		}
		return OpenedChart{Archive: chart}, nil
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
		return OpenedChart{Archive: src}, err
	}
}

func (s *ChartArchiveSource) openLocalBlob(ctx context.Context, req ChartRequest) (OpenedChart, error) {
	// A specific media type of the buffered content wins: repositories store a local blob holding an OCI
	// artifact as a native manifest (descriptor mediaType application/vnd.oci.image.manifest.v1+json)
	// and hand it out as an OCM OCI layout.
	var mediaType string
	if mt, ok := req.Buffered.(blob.MediaTypeAware); ok {
		mediaType, _ = mt.MediaType()
	}
	if mediaType == "" || mediaType == "application/octet-stream" {
		mediaType = localBlobMediaType(req.Resource.Access)
	}
	base := strings.TrimSpace(strings.SplitN(mediaType, ";", 2)[0])
	switch {
	case isChartArchiveMediaType(base):
		if err := verifyArchive(req.Buffered, req.Target); err != nil {
			return OpenedChart{}, err
		}
		return OpenedChart{Archive: req.Buffered}, nil
	case strings.HasPrefix(base, layout.MediaTypeOCIImageLayoutV1), base == ocispec.MediaTypeImageManifest:
		store, err := ocitar.ReadOCILayout(ctx, req.Buffered)
		if err != nil {
			return OpenedChart{}, fmt.Errorf("failed reading OCI layout of resource %s: %w", req.Resource.ToIdentity(), err)
		}
		defer func() { _ = store.Close() }()
		roots := store.MainArtifacts(ctx)
		if len(roots) != 1 {
			return OpenedChart{}, fmt.Errorf("OCI layout of resource %s must contain exactly one artifact, got %d", req.Resource.ToIdentity(), len(roots))
		}
		layer, err := chartLayer(ctx, store, roots[0], req)
		if err != nil {
			return OpenedChart{}, err
		}
		// The layout is read from the buffered file and closed on return, so the (small) chart
		// layer is materialized in memory to outlive it.
		rc, err := layer.ReadCloser()
		if err != nil {
			return OpenedChart{}, err
		}
		defer func() { _ = rc.Close() }()
		data, err := io.ReadAll(rc)
		if err != nil {
			return OpenedChart{}, fmt.Errorf("failed reading helm chart layer of resource %s: %w", req.Resource.ToIdentity(), err)
		}
		return OpenedChart{
			Archive: inmemory.New(bytes.NewReader(data), inmemory.WithSize(int64(len(data))), inmemory.WithMediaType(layer.desc.MediaType)),
			Derived: true,
		}, nil
	default:
		return OpenedChart{}, fmt.Errorf("local blob of resource %s has media type %q, which is neither a packaged helm chart (%s) nor an OCI layout (%s)",
			req.Resource.ToIdentity(), mediaType, strings.Join(chartArchiveMediaTypes, ", "), layout.MediaTypeOCIImageLayoutV1)
	}
}

// chartLayer resolves the helm chart layer of the OCI image manifest root in store, after
// checking the chart metadata from the helm config blob against req.Target.
func chartLayer(ctx context.Context, store content.Fetcher, root ocispec.Descriptor, req ChartRequest) (*chartLayerBlob, error) {
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
		if err := verifyMetadata(meta, req.Target); err != nil {
			return nil, err
		}
	}
	for _, layer := range manifest.Layers {
		if layer.MediaType == registry.ChartLayerMediaType || layer.MediaType == registry.LegacyChartLayerMediaType {
			return &chartLayerBlob{
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

// verifyArchive reads the chart metadata from a re-readable chart archive and checks it
// against target.
func verifyArchive(archive blob.ReadOnlyBlob, target *descriptor.Resource) error {
	name, version, ok := publishedChart(target)
	if !ok {
		return nil
	}
	rc, err := archive.ReadCloser()
	if err != nil {
		return fmt.Errorf("failed opening chart archive: %w", err)
	}
	defer func() { _ = rc.Close() }()
	chart, err := loader.LoadArchive(rc)
	if err != nil {
		return fmt.Errorf("failed loading chart archive: %w", err)
	}
	if chart.Metadata == nil {
		return errors.New("chart archive has no Chart.yaml metadata")
	}
	return compareChart(chartMetadata{Name: chart.Metadata.Name, Version: chart.Metadata.Version}, name, version)
}

func verifyMetadata(meta chartMetadata, target *descriptor.Resource) error {
	name, version, ok := publishedChart(target)
	if !ok {
		return nil
	}
	return compareChart(meta, name, version)
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

// chartLayerBlob streams a single OCI layer on each ReadCloser call without buffering it.
type chartLayerBlob struct {
	fetch func() (io.ReadCloser, error)
	desc  ocispec.Descriptor
}

var (
	_ blob.SizeAware   = (*chartLayerBlob)(nil)
	_ blob.DigestAware = (*chartLayerBlob)(nil)
)

func (b *chartLayerBlob) ReadCloser() (io.ReadCloser, error) { return b.fetch() }

func (b *chartLayerBlob) Size() int64 { return b.desc.Size }

func (b *chartLayerBlob) Digest() (string, bool) { return b.desc.Digest.String(), true }
