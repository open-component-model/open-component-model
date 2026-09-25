package transformation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v4/pkg/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	helmblob "ocm.software/open-component-model/bindings/go/helm/blob"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ChartArchiveOpener is the HTTPStreamingSpec.Opener name under which ChartArchiveSource.Open is registered.
const ChartArchiveOpener = "HelmChartArchive"

// ChartArchiveSource opens the packaged Helm chart archive (.tgz) of a resource so it can be
// streamed to a classic Helm repository such as JFrog Artifactory.
type ChartArchiveSource struct {
	// ResourceRepository downloads Helm/v1 and all non-OCI sources.
	ResourceRepository repository.ResourceRepository
	// OCIRepository streams the chart layer of OCIImage sources directly from the registry.
	OCIRepository ocistream.ResourceRepository
}

// Open returns the chart archive of resource:
//   - Helm access: the chart .tgz extracted from the helm download (the provenance file is dropped).
//   - OCIImage access: the helm chart layer, streamed lazily from the registry.
//   - any other access: the downloaded bytes unchanged, which must already be the chart .tgz.
func (s *ChartArchiveSource) Open(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	if resource == nil || resource.Access == nil {
		return nil, errors.New("resource access is required")
	}
	typ := resource.Access.GetType()
	switch {
	case helmaccess.Scheme.IsRegistered(typ):
		src, err := s.ResourceRepository.DownloadResource(ctx, resource, credentials)
		if err != nil {
			return nil, fmt.Errorf("failed downloading helm chart: %w", err)
		}
		chart, err := helmblob.NewChartBlob(src).ChartArchive()
		if err != nil {
			return nil, fmt.Errorf("failed extracting chart archive from helm download: %w", err)
		}
		return chart, nil
	case isOCIImage(typ):
		return s.openOCIChartLayer(ctx, resource, credentials)
	default:
		return s.ResourceRepository.DownloadResource(ctx, resource, credentials)
	}
}

func (s *ChartArchiveSource) openOCIChartLayer(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	stream, err := s.OCIRepository.DownloadResourceStream(ctx, resource, credentials)
	if err != nil {
		return nil, fmt.Errorf("failed resolving OCI artifact of resource %s: %w", resource.ToIdentity(), err)
	}
	root := stream.Root()
	if root.MediaType != ocispec.MediaTypeImageManifest {
		return nil, fmt.Errorf("helm chart of resource %s must be an OCI image manifest, got %q", resource.ToIdentity(), root.MediaType)
	}
	rc, err := stream.Fetch(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("failed fetching OCI manifest of resource %s: %w", resource.ToIdentity(), err)
	}
	var manifest ocispec.Manifest
	err = json.NewDecoder(rc).Decode(&manifest)
	_ = rc.Close()
	if err != nil {
		return nil, fmt.Errorf("failed decoding OCI manifest of resource %s: %w", resource.ToIdentity(), err)
	}
	for _, layer := range manifest.Layers {
		if layer.MediaType == registry.ChartLayerMediaType || layer.MediaType == registry.LegacyChartLayerMediaType {
			return &chartLayerBlob{
				fetch: func() (io.ReadCloser, error) { return stream.Fetch(ctx, layer) },
				desc:  layer,
			}, nil
		}
	}
	return nil, fmt.Errorf("OCI artifact of resource %s has no helm chart layer (%s)", resource.ToIdentity(), registry.ChartLayerMediaType)
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
