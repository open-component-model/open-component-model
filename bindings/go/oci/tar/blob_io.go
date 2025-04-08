package tar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
)

type CopyOCILayoutOptions struct {
	oras.CopyGraphOptions
	MutateIndexFunc func(*ociImageSpecV1.Descriptor) error
}

func CopyOCILayout(ctx context.Context, dst content.Storage, src blob.ReadOnlyBlob, opts CopyOCILayoutOptions) (index ociImageSpecV1.Descriptor, err error) {
	ociStore, err := ReadOCILayout(ctx, src)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to read OCI layout: %w", err)
	}
	defer func() {
		err = errors.Join(err, ociStore.Close())
	}()

	indexJSON, err := json.Marshal(ociStore.Index)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to marshal index: %w", err)
	}
	index = content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, indexJSON)
	if err := opts.MutateIndexFunc(&index); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to mutate index descriptor before copy: %w", err)
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

	if err := oras.CopyGraph(ctx, proxy, dst, index, opts.CopyGraphOptions); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to copy graph for index from oci layout %v: %w", index, err)
	}

	return index, nil
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
