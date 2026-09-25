// Package chartarchive opens the packaged Helm chart (.tgz) of an OCM resource as a stream. It
// is used to deploy charts to classic (index.yaml based) Helm repositories without buffering
// them on disk. The chart is located, not parsed: the target repository reads its metadata.
package chartarchive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/helm/internal/download"
	helmaccess "ocm.software/open-component-model/bindings/go/helm/spec/access"
	helmaccessv1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Source opens the packaged Helm chart (.tgz) of a resource as a stream.
type Source struct {
	// ResourceRepository downloads remote resources that cannot be streamed otherwise.
	ResourceRepository repository.ResourceRepository
	// OCIRepository streams OCIImage resources and oci:// Helm charts.
	OCIRepository ocistream.ResourceRepository
	// HTTPConfig configures the client streaming charts from HTTP/S Helm repositories.
	HTTPConfig *httpv1alpha1.Config
	// Streamers open other remote resources as streams. The first one that does not
	// return errors.ErrUnsupported wins; without one, ResourceRepository downloads the resource.
	Streamers []Streamer
}

// Streamer opens a remote resource as a stream, returning the content and its size (-1 when
// unknown). It returns errors.ErrUnsupported for access types it does not handle.
type Streamer interface {
	OpenResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (io.ReadCloser, int64, error)
}

// Local locates a local resource in its component version.
type Local struct {
	Repository repository.ComponentVersionRepository
	Component  string
	Version    string
}

// Request describes the resource to open.
type Request struct {
	Resource    *descriptor.Resource
	Credentials runtime.Typed // resolved source credentials (remote resources only)
	Local       *Local        // set for local blob resources
}

// Chart is an opened chart archive.
type Chart struct {
	// Archive yields the chart .tgz exactly once; it implements blob.SizeAware
	// (blob.SizeUnknown when the size is unknown), and blob.DigestAware when the digest of the
	// archive is known up front (OCI chart layers).
	Archive blob.ReadOnlyBlob
	// FromOCI reports that Archive was extracted from an OCI artifact, so the source
	// resource digest does not describe Archive. This includes oci:// Helm charts, whose
	// resource digest is the manifest digest (see helm/digest), not that of the chart layer.
	FromOCI bool
}

// Close releases the source stream when Archive was never read, e.g. because the target
// already had the chart. It is a no-op once Archive was read.
func (c *Chart) Close() error {
	if closer, ok := c.Archive.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// content is the fetched resource: either a lazy OCI stream or a blob, with an optional media
// type hint for the blob.
type content struct {
	stream    ocistream.ResourceStream
	blob      blob.ReadOnlyBlob
	mediaType string
}

// localResourceStreamer is implemented by OCI and CTF component version repositories.
type localResourceStreamer interface {
	GetLocalResourceStream(ctx context.Context, component, version string, identity runtime.Identity) (ocistream.ResourceStream, *descriptor.Resource, error)
}

// Open fetches the resource, streaming it wherever the source allows, and locates the packaged
// chart in its content: a helm chart OCI artifact (its chart layer), a packaged chart, a tar
// containing a packaged chart (as produced by the helm downloader) or an OCM OCI layout.
// The access type only decides how the bytes are fetched, never how they are interpreted.
func (s *Source) Open(ctx context.Context, req Request) (*Chart, error) {
	if req.Resource == nil || req.Resource.Access == nil {
		return nil, errors.New("resource access is required")
	}
	id := req.Resource.ToIdentity()
	c, err := s.fetch(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed opening resource %s: %w", id, err)
	}
	return detect(ctx, c, id)
}

func (s *Source) fetch(ctx context.Context, req Request) (content, error) {
	if req.Local != nil {
		return fetchLocal(ctx, req)
	}
	typ := req.Resource.Access.GetType()
	switch {
	case helmaccess.Scheme.IsRegistered(typ):
		return s.fetchHelm(ctx, req)
	case isOCIImage(typ):
		stream, err := s.OCIRepository.DownloadResourceStream(ctx, req.Resource, req.Credentials)
		return content{stream: stream}, err
	default:
		return s.openRemote(ctx, req)
	}
}

func fetchLocal(ctx context.Context, req Request) (content, error) {
	local, id := req.Local, req.Resource.ToIdentity()
	if streamer, ok := local.Repository.(localResourceStreamer); ok {
		stream, _, err := streamer.GetLocalResourceStream(ctx, local.Component, local.Version, id)
		return content{stream: stream}, err
	}
	b, _, err := local.Repository.GetLocalResource(ctx, local.Component, local.Version, id)
	if err != nil {
		return content{}, err
	}
	mediaType := localBlobMediaType(req.Resource.Access)
	if mt, ok := b.(blob.MediaTypeAware); ok {
		if m, known := mt.MediaType(); known && m != "" && m != "application/octet-stream" {
			mediaType = m
		}
	}
	return content{blob: b, mediaType: mediaType}, nil
}

func (s *Source) fetchHelm(ctx context.Context, req Request) (content, error) {
	var access helmaccessv1.Helm
	if err := helmaccess.Scheme.Convert(req.Resource.Access, &access); err != nil {
		return content{}, fmt.Errorf("error converting access to helm spec: %w", err)
	}
	ref, err := access.ChartReference()
	if err != nil {
		return content{}, fmt.Errorf("error constructing chart reference: %w", err)
	}

	if ociRef, ok := strings.CutPrefix(ref, "oci://"); ok {
		ociResource := req.Resource.DeepCopy()
		ociResource.Access = &ociaccessv1.OCIImage{
			Type:           runtime.NewVersionedType(ociaccessv1.OCIImageType, ociaccessv1.Version),
			ImageReference: ociRef,
		}
		var creds runtime.Typed
		if req.Credentials != nil {
			ociCreds, err := helmcredsv1.ConvertToOCICredentials(req.Credentials)
			if err != nil {
				return content{}, fmt.Errorf("error converting credentials: %w", err)
			}
			creds = ociCreds
		}
		stream, err := s.OCIRepository.DownloadResourceStream(ctx, ociResource, creds)
		return content{stream: stream}, err
	}

	opts := []download.Option{download.WithHTTPConfig(s.HTTPConfig)}
	if req.Credentials != nil {
		creds, err := helmcredsv1.ConvertToHelmHTTPCredentials(req.Credentials)
		if err != nil {
			return content{}, fmt.Errorf("error converting credentials: %w", err)
		}
		opts = append(opts, download.WithCredentials(creds))
	}
	rc, size, err := download.OpenHTTPChart(ctx, ref, opts...)
	switch {
	case errors.Is(err, download.ErrNotStreamable):
		// Client certificates, custom CAs or provenance verification need the helm downloader,
		// which works on files and yields a tar of the chart and its provenance file.
		slog.DebugContext(ctx, "helm chart cannot be streamed, downloading it", "chart", ref)
		return s.download(ctx, req)
	case err != nil:
		return content{}, fmt.Errorf("failed streaming helm chart: %w", err)
	}
	if size < 0 {
		size = blob.SizeUnknown
	}
	return content{blob: &readerBlob{rc: rc, size: size}}, nil
}

// openRemote streams the resource with the first Streamer supporting it, else downloads it.
func (s *Source) openRemote(ctx context.Context, req Request) (content, error) {
	for _, streamer := range s.Streamers {
		rc, size, err := streamer.OpenResource(ctx, req.Resource, req.Credentials)
		switch {
		case errors.Is(err, errors.ErrUnsupported):
			continue
		case err != nil:
			return content{}, err
		}
		if size < 0 {
			size = blob.SizeUnknown
		}
		return content{blob: &readerBlob{rc: rc, size: size}}, nil
	}
	return s.download(ctx, req)
}

func (s *Source) download(ctx context.Context, req Request) (content, error) {
	b, err := s.ResourceRepository.DownloadResource(ctx, req.Resource, req.Credentials)
	return content{blob: b}, err
}

func localBlobMediaType(access runtime.Typed) string {
	var lb descriptorv2.LocalBlob
	if err := descriptorv2.Scheme.Convert(access, &lb); err != nil {
		return ""
	}
	return lb.MediaType
}

func isOCIImage(typ runtime.Type) bool {
	obj, err := ociaccess.Scheme.NewObject(typ)
	if err != nil {
		return false
	}
	_, ok := obj.(*ociaccessv1.OCIImage)
	return ok
}

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
