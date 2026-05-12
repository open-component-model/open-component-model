package stream

import (
	"context"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

// OCIResourceStream wraps a content.ReadOnlyStorage (typically a remote.Repository)
// and a resolved root descriptor. No network I/O occurs at construction time.
type OCIResourceStream struct {
	store    content.ReadOnlyStorage
	root     ocispec.Descriptor
	copyOpts oras.CopyGraphOptions
	tempDir  string
	tags     []string
}

var _ ResourceStream = (*OCIResourceStream)(nil)

// New creates a ResourceStream from an oras ReadOnlyStorage and a resolved root descriptor.
func New(store content.ReadOnlyStorage, root ocispec.Descriptor, copyOpts oras.CopyGraphOptions, tempDir string, tags []string) *OCIResourceStream {
	return &OCIResourceStream{
		store:    store,
		root:     root,
		copyOpts: copyOpts,
		tempDir:  tempDir,
		tags:     tags,
	}
}

func (s *OCIResourceStream) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	return s.store.Fetch(ctx, desc)
}

func (s *OCIResourceStream) Exists(ctx context.Context, desc ocispec.Descriptor) (bool, error) {
	return s.store.Exists(ctx, desc)
}

func (s *OCIResourceStream) Root() ocispec.Descriptor {
	return s.root
}

func (s *OCIResourceStream) Materialize(ctx context.Context) (blob.ReadOnlyBlob, error) {
	return tar.CopyToOCILayoutInMemory(ctx, s.store, s.root, tar.CopyToOCILayoutOptions{
		CopyGraphOptions: s.copyOpts,
		Tags:             s.tags,
		TempDir:          s.tempDir,
	})
}
