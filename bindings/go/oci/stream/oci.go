package stream

import (
	"context"
	"io"
	"sync"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// OCIResourceStream wraps a content.ReadOnlyGraphStorage (typically a
// remote.Repository) and a resolved root descriptor. No network I/O occurs at
// construction time. Tags are OCI reference tags applied to the layout during
// Materialize (passed to tar.CopyToOCILayoutOptions). For remote refs they
// should be the full ImageReference string so the caller can resolve the
// layout by that same key.
//
// ExtendedCopyOpts drives Materialize's oras.ExtendedCopyGraph. The zero value
// uses oras's defaults: src.Predecessors and unbounded Depth, so every
// referrer of Descriptor rides along into the layout.
type OCIResourceStream struct {
	content.ReadOnlyGraphStorage
	Descriptor       ocispec.Descriptor
	ExtendedCopyOpts oras.ExtendedCopyGraphOptions
	TempDir          string
	Tags             []string
}

var _ ResourceStream = (*OCIResourceStream)(nil)

func (s *OCIResourceStream) Root() ocispec.Descriptor {
	return s.Descriptor
}

// Materialize returns a [LayoutBlob] over s: an OCI layout tar containing
// Descriptor and the predecessors reachable via ExtendedCopyOpts, built only
// when the blob is read. The walk is entirely controlled by the caller via
// ExtendedCopyOpts.
func (s *OCIResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
	return NewLayoutBlob(ctx, s), nil
}

// LayoutBlob is the gzipped OCI layout tar of an [OCIResourceStream]. It is
// also the stream itself: consumers that copy OCI graphs (adding a local
// resource, uploading an OCI image) detect [ResourceStream] and copy from the
// source store directly. The tar is only built, through a temporary file and
// into memory, on the first read of the blob, so those consumers never pay
// for it.
type LayoutBlob struct {
	*OCIResourceStream
	tarball func() (*inmemory.Blob, error)
}

var (
	_ ResourceStream      = (*LayoutBlob)(nil)
	_ blob.ReadOnlyBlob   = (*LayoutBlob)(nil)
	_ blob.SizeAware      = (*LayoutBlob)(nil)
	_ blob.DigestAware    = (*LayoutBlob)(nil)
	_ blob.MediaTypeAware = (*LayoutBlob)(nil)
)

// NewLayoutBlob returns a [LayoutBlob] over s. ctx is used for the copy that
// builds the tar, which starts on the first read of the blob.
func NewLayoutBlob(ctx context.Context, s *OCIResourceStream) *LayoutBlob {
	return &LayoutBlob{
		OCIResourceStream: s,
		tarball: sync.OnceValues(func() (*inmemory.Blob, error) {
			return tar.CopyToOCILayoutInMemory(ctx, s.ReadOnlyGraphStorage, s.Descriptor, tar.CopyToOCILayoutOptions{
				ExtendedCopyGraphOptions: s.ExtendedCopyOpts,
				Tags:                     s.Tags,
				TempDir:                  s.TempDir,
			})
		}),
	}
}

// Materialize returns b itself, so the tar is built at most once.
func (b *LayoutBlob) Materialize(context.Context) (blob.ReadOnlyBlob, error) {
	return b, nil
}

func (b *LayoutBlob) ReadCloser() (io.ReadCloser, error) {
	l, err := b.tarball()
	if err != nil {
		return nil, err
	}
	return l.ReadCloser()
}

// Size builds the tar to measure it.
func (b *LayoutBlob) Size() int64 {
	l, err := b.tarball()
	if err != nil {
		return blob.SizeUnknown
	}
	return l.Size()
}

// Digest builds the tar to hash it.
func (b *LayoutBlob) Digest() (string, bool) {
	l, err := b.tarball()
	if err != nil {
		return "", false
	}
	return l.Digest()
}

func (b *LayoutBlob) MediaType() (string, bool) {
	return layout.MediaTypeOCIImageLayoutTarGzipV1, true
}
