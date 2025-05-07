// Package fetch provides core functionality for retrieving OCI artifacts and their contents.
// It handles the low-level operations of fetching manifests, layers, and blobs from OCI registries.
package fetch

import (
	"context"
	"encoding/json"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"

	"ocm.software/open-component-model/bindings/go/blob"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
)

// LocalBlob represents a content-addressable piece of data stored in an OCI repository.
// It provides a unified interface for accessing both the content and metadata of OCI blobs.
type LocalBlob interface {
	blob.ReadOnlyBlob
	blob.SizeAware
	blob.DigestAware
	blob.MediaTypeAware
}

// SingleLayerLocalBlobFromManifestByIdentity finds and retrieves a specific layer from an OCI manifest
// based on its identity annotations. This is useful for extracting specific resources from OCI artifacts.
//
// Parameters:
//   - ctx: Context for the operation
//   - store: The OCI store to fetch from
//   - manifest: The OCI manifest containing the layers
//   - identity: Map of annotations used to identify the desired layer
//
// Returns:
//   - A LocalBlob representing the matching layer
//   - An error if the layer cannot be found or fetched
func SingleLayerLocalBlobFromManifestByIdentity(ctx context.Context, store oras.ReadOnlyTarget, manifest *ociImageSpecV1.Manifest, identity map[string]string, artifactKind annotations.ArtifactKind) (LocalBlob, error) {
	layer, err := annotations.FilterFirstMatchingArtifact(manifest.Layers, identity, artifactKind)
	if err != nil {
		return nil, fmt.Errorf("failed to find matching layer: %w", err)
	}
	data, err := store.Fetch(ctx, layer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer data: %w", err)
	}
	if data == nil {
		return nil, fmt.Errorf("received nil data for layer %s", layer.Digest)
	}
	return ociblob.NewDescriptorBlob(data, layer), nil
}

// SingleLayerManifestBlobFromIndex locates a specific manifest in an OCI index and retrieves its first layer.
// This is commonly used to access the primary content of a multi-arch OCI artifact.
//
// Parameters:
//   - ctx: Context for the operation
//   - store: The OCI store to fetch from
//   - index: The OCI index containing the manifests
//   - identity: Map of annotations used to identify the desired manifest
//
// Returns:
//   - A LocalBlob representing the first layer of the matching manifest
//   - An error if the manifest cannot be found or its layer cannot be fetched
func SingleLayerManifestBlobFromIndex(ctx context.Context, store oras.ReadOnlyTarget, index *ociImageSpecV1.Index, identity map[string]string, artifactKind annotations.ArtifactKind) (LocalBlob, error) {
	manifestDesc, err := annotations.FilterFirstMatchingArtifact(index.Manifests, identity, artifactKind)
	if err != nil {
		return nil, fmt.Errorf("failed to find matching layer: %w", err)
	}
	return SingleLayerManifestBlobFromManifestDescriptor(ctx, store, manifestDesc)
}

// SingleLayerManifestBlobFromManifestDescriptor retrieves the primary content layer from a manifest.
// This is the standard way to access the main content of an OCI artifact.
//
// Parameters:
//   - ctx: Context for the operation
//   - store: The OCI store to fetch from
//   - manifestDesc: The descriptor of the manifest to fetch
//
// Returns:
//   - A LocalBlob representing the first layer of the manifest
//   - An error if the manifest or its layer cannot be fetched
func SingleLayerManifestBlobFromManifestDescriptor(ctx context.Context, store oras.ReadOnlyTarget, manifestDesc ociImageSpecV1.Descriptor) (LocalBlob, error) {
	manifest, err := Manifest(ctx, store, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest data: %w", err)
	}
	layerDesc, err := LayerFromManifest(manifest, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer data: %w", err)
	}
	layerData, err := store.Fetch(ctx, layerDesc)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch layer data: %w", err)
	}

	return ociblob.NewDescriptorBlob(layerData, layerDesc), nil
}

// LayerFromManifest provides access to a specific layer within an OCI manifest.
// It handles validation of layer indices and ensures safe access to manifest contents.
//
// Parameters:
//   - manifest: The OCI manifest containing the layers
//   - layer: The index of the layer to retrieve (0-based)
//
// Returns:
//   - The descriptor of the requested layer
//   - An error if the layer index is invalid or the manifest has no layers
func LayerFromManifest(manifest ociImageSpecV1.Manifest, layer int) (ociImageSpecV1.Descriptor, error) {
	if len(manifest.Layers) == 0 {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("manifest has no layers and cannot be used to get a local blob")
	}
	if layer >= len(manifest.Layers) || layer < 0 {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("layer at index %d does not exist in manifest (%d layers in total)", layer, len(manifest.Layers))
	}
	return manifest.Layers[layer], nil
}

// Manifest decodes an OCI manifest from its descriptor.
// This is the entry point for accessing the structure and contents of an OCI artifact.
//
// Parameters:
//   - ctx: Context for the operation
//   - store: The OCI store to fetch from
//   - desc: The descriptor of the manifest to fetch
//
// Returns:
//   - The decoded OCI manifest
//   - An error if the manifest cannot be fetched or decoded
func Manifest(ctx context.Context, store oras.ReadOnlyTarget, desc ociImageSpecV1.Descriptor) (ociImageSpecV1.Manifest, error) {
	data, err := store.Fetch(ctx, desc)
	if err != nil {
		return ociImageSpecV1.Manifest{}, fmt.Errorf("failed to fetch layer data: %w", err)
	}
	defer func() {
		_ = data.Close()
	}()
	manifest := ociImageSpecV1.Manifest{}
	if err := json.NewDecoder(data).Decode(&manifest); err != nil {
		return ociImageSpecV1.Manifest{}, fmt.Errorf("failed to decode manifest: %w", err)
	}
	return manifest, nil
}
