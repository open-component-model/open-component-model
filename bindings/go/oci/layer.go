package oci

import (
	"context"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
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

// fetchLayer returns a single layer blob as raw content. A layer is not an OCI artifact,
// so it is never wrapped in an OCI layout.
//
// The layer is fetched on every read with ctx, and every read is verified against the
// digest and size of desc, so the blob holds neither the content nor an open stream.
func fetchLayer(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) blob.ReadOnlyBlob {
	src := blob.ReadOnlyBlobFunc(func() (io.ReadCloser, error) {
		// Without a media type, oras always fetches from the blob endpoint. A layer is a blob,
		// even if its declared media type is that of a manifest.
		reader, err := fetcher.Fetch(ctx, ociImageSpecV1.Descriptor{Digest: desc.Digest, Size: desc.Size})
		if err != nil {
			return nil, fmt.Errorf("failed to fetch layer %q: %w", desc.Digest, err)
		}
		return reader, nil
	})
	if desc.MediaType == "" {
		desc.MediaType = defaultLayerMediaType
	}
	return ociblob.NewDescriptorBlob(src, desc)
}

// defaultLayerMediaType is reported for a layer whose access declares no media type.
const defaultLayerMediaType = "application/octet-stream"
