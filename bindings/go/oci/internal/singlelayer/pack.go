package singlelayer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"

	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
)

type PackOptions struct {
	Main         ociImageSpecV1.Descriptor
	ArtifactType string
}

func PackSingleLayerOCIArtifact(ctx context.Context, storage content.Storage, b blob.ReadOnlyBlob, opts PackOptions) (desc ociImageSpecV1.Descriptor, err error) {
	layerData, err := b.ReadCloser()
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to get b reader: %w", err)
	}
	defer func() {
		err = errors.Join(err, layerData.Close())
	}()

	if err := storage.Push(ctx, opts.Main, io.NopCloser(layerData)); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to push layer: %w", err)
	}
	if exists, err := storage.Exists(ctx, ociImageSpecV1.DescriptorEmptyJSON); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to check if layer exists: %w", err)
	} else if !exists {
		if err := storage.Push(ctx, ociImageSpecV1.DescriptorEmptyJSON, bytes.NewReader(ociImageSpecV1.DescriptorEmptyJSON.Data)); err != nil {
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to push empty layer: %w", err)
		}
	}

	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: opts.ArtifactType,
		Config:       ociImageSpecV1.DescriptorEmptyJSON,
		Layers: []ociImageSpecV1.Descriptor{
			opts.Main,
		},
		Annotations: opts.Main.Annotations,
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to marshal manifest: %w", err)
	}
	manifestDescriptor := content.NewDescriptorFromBytes(manifest.MediaType, manifestJSON)
	manifestDescriptor.Annotations = maps.Clone(manifest.Annotations)
	if err := storage.Push(ctx, manifestDescriptor, bytes.NewReader(manifestJSON)); err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to push manifest: %w", err)
	}

	return manifestDescriptor, nil
}
