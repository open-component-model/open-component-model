package tar

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

type CopyToOCILayoutOptions struct {
	oras.CopyGraphOptions
	Tags []string
}

func CopyToOCILayoutInMemory(ctx context.Context, src content.ReadOnlyStorage, base ociImageSpecV1.Descriptor, opts CopyToOCILayoutOptions) (b *inmemory.Blob, err error) {
	var buf bytes.Buffer

	h := sha256.New()
	writer := io.MultiWriter(&buf, h)

	zippedBuf := gzip.NewWriter(writer)
	defer func() {
		if err != nil {
			// Clean up resources if there was an error
			zippedBuf.Close()
			buf.Reset()
		}
	}()

	target := NewOCILayoutWriter(zippedBuf)
	defer func() {
		if terr := target.Close(); terr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close tar writer: %w", terr))
			return
		}
		if zerr := zippedBuf.Close(); zerr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close gzip writer: %w", zerr))
			return
		}
	}()

	if err := oras.CopyGraph(ctx, src, target, base, opts.CopyGraphOptions); err != nil {
		return nil, fmt.Errorf("failed to copy graph starting from descriptor %v: %w", base, err)
	}

	for _, tag := range opts.Tags {
		if err := target.Tag(ctx, base, tag); err != nil {
			return nil, fmt.Errorf("failed to tag base: %w", err)
		}
	}

	// now close prematurely so that the buf is fully filled before we set things like size and digest.
	if err := errors.Join(target.Close(), zippedBuf.Close()); err != nil {
		return nil, fmt.Errorf("failed to close writers: %w", err)
	}

	downloaded := inmemory.New(&buf)

	downloaded.SetPrecalculatedSize(int64(buf.Len()))
	downloaded.SetMediaType(layout.MediaTypeOCIImageLayoutTarGzipV1)

	blobDigest := digest.NewDigest(digest.SHA256, h)
	downloaded.SetPrecalculatedDigest(blobDigest.String())

	return downloaded, nil
}

type CopyOCILayoutWithIndexOptions struct {
	oras.CopyGraphOptions
	MutateParentFunc func(*ociImageSpecV1.Descriptor) error
}

func CopyOCILayoutWithIndex(ctx context.Context, dst content.Storage, src blob.ReadOnlyBlob, opts CopyOCILayoutWithIndexOptions) (top ociImageSpecV1.Descriptor, err error) {
	ociStore, err := ReadOCILayout(ctx, src)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to read OCI layout: %w", err)
	}
	defer func() {
		err = errors.Join(err, ociStore.Close())
	}()

	index, proxy, err := proxyOCIStore(ctx, ociStore, &opts)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to create proxy for OCI store: %w", err)
	}

	if err := oras.CopyGraph(ctx, proxy, dst, index, opts.CopyGraphOptions); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to copy graph for index from oci layout %v: %w", index, err)
	}

	return index, nil
}

func proxyOCIStore(ctx context.Context, ociStore *CloseableReadOnlyStore, opts *CopyOCILayoutWithIndexOptions) (ociImageSpecV1.Descriptor, content.ReadOnlyStorage, error) {
	// if our store only has one single manifest, we dont need to copy the index, instead we can use the manifest as is.
	if len(ociStore.Index.Manifests) == 1 {
		return proxyOCIStoreWithManifest(ctx, ociStore, opts)
	}
	// if there is more than one manifest in the store, we are dealing with multiple artifacts, so in this case we should also copy the index
	// TODO(jakobmoellerdev): It might make sense here to split this into multiple manifests without a top level index as well.
	//  Currently the use cases are too unclear to decide here, but we can revisit this at any time and switch it quite easily.
	return proxyOCIStoreWithIndex(ociStore, opts)
}

func proxyOCIStoreWithIndex(ociStore *CloseableReadOnlyStore, opts *CopyOCILayoutWithIndexOptions) (ociImageSpecV1.Descriptor, content.ReadOnlyStorage, error) {
	indexJSON, err := json.Marshal(ociStore.Index)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to marshal index: %w", err)
	}
	index := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, indexJSON)
	if err := opts.MutateParentFunc(&index); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to mutate index descriptor before copy: %w", err)
	}

	opts.FindSuccessors = func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
		if content.Equal(desc, index) {
			return ociStore.Index.Manifests, nil
		}
		return content.Successors(ctx, ociStore, desc)
	}

	proxy := &descriptorStoreProxy{
		raw:             indexJSON,
		desc:            index,
		ReadOnlyStorage: ociStore,
	}
	return index, proxy, nil
}

func proxyOCIStoreWithManifest(ctx context.Context, ociStore *CloseableReadOnlyStore, opts *CopyOCILayoutWithIndexOptions) (ociImageSpecV1.Descriptor, content.ReadOnlyStorage, error) {
	manifestDesc := ociStore.Index.Manifests[0]
	manifestRawStream, err := ociStore.Fetch(ctx, manifestDesc)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	manifestRaw, err := content.ReadAll(manifestRawStream, manifestDesc)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to read manifest: %w", err)
	}
	var manifest ociImageSpecV1.Manifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to unmarshal manifest: %w", err)
	}
	if err := opts.MutateParentFunc(&manifestDesc); err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to mutate manifest descriptor before copy: %w", err)
	}
	opts.FindSuccessors = func(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
		if content.Equal(desc, manifestDesc) {
			return append([]ociImageSpecV1.Descriptor{manifest.Config}, manifest.Layers...), nil
		}
		return content.Successors(ctx, ociStore, desc)
	}
	proxy := &descriptorStoreProxy{
		raw:             manifestRaw,
		desc:            manifestDesc,
		ReadOnlyStorage: ociStore,
	}
	return manifestDesc, proxy, nil
}

type descriptorStoreProxy struct {
	raw  []byte
	desc ociImageSpecV1.Descriptor
	content.ReadOnlyStorage
}

func (p *descriptorStoreProxy) Exists(ctx context.Context, desc ociImageSpecV1.Descriptor) (bool, error) {
	if p.desc.Digest.String() == desc.Digest.String() {
		return true, nil
	}
	return p.ReadOnlyStorage.Exists(ctx, desc)
}

func (p *descriptorStoreProxy) Fetch(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	if p.desc.Digest.String() == desc.Digest.String() {
		return io.NopCloser(bytes.NewReader(p.raw)), nil
	}
	return p.ReadOnlyStorage.Fetch(ctx, desc)
}
