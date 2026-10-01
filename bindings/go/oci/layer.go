package oci

import (
	"context"
	"errors"
	"fmt"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
)

// layerDescriptor is the OCI descriptor an OCIImageLayer access addresses.
func layerDescriptor(access *accessv1.OCIImageLayer) ociImageSpecV1.Descriptor {
	return ociImageSpecV1.Descriptor{
		MediaType: access.MediaType,
		Digest:    access.Digest,
		Size:      access.Size,
	}
}

// resolveLayer resolves the blob a layer addresses. Remote repositories resolve against
// their blob endpoint, because the generic resolver of an oras remote repository always
// queries the manifest endpoint.
func resolveLayer(ctx context.Context, store spec.Store, dig digest.Digest) (ociImageSpecV1.Descriptor, error) {
	resolve := store.Resolve
	if bs, ok := store.(interface{ Blobs() registry.BlobStore }); ok {
		resolve = bs.Blobs().Resolve
	}
	return resolve(ctx, dig.String())
}

// fetchLayer fetches a single layer blob as raw content and verifies its digest and size.
// A layer is not an OCI artifact, so it is never wrapped in an OCI layout.
func fetchLayer(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) (b blob.ReadOnlyBlob, err error) {
	// Without a media type, oras always fetches from the blob endpoint. A layer is a blob,
	// even if its declared media type is that of a manifest.
	reader, err := fetcher.Fetch(ctx, ociImageSpecV1.Descriptor{Digest: desc.Digest, Size: desc.Size})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer %q: %w", desc.Digest, err)
	}
	defer func() {
		err = errors.Join(err, reader.Close())
	}()

	opts := []inmemory.MemoryBlobOption{
		inmemory.WithDigest(desc.Digest.String()),
	}
	// An empty media type would overwrite the default the blob applies on its own.
	if desc.MediaType != "" {
		opts = append(opts, inmemory.WithMediaType(desc.MediaType))
	}
	layer := inmemory.New(reader, opts...)
	if err := layer.Load(); err != nil {
		return nil, fmt.Errorf("failed to read layer %q: %w", desc.Digest, err)
	}
	if layer.Size() != desc.Size {
		return nil, fmt.Errorf("layer %q has size %d, but the descriptor declares %d", desc.Digest, layer.Size(), desc.Size)
	}
	return layer, nil
}
