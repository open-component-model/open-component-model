package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/log"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// storeDescriptorOptions defines the options for adding a component descriptor to a Store.
type storeDescriptorOptions struct {
	Scheme                        *runtime.Scheme
	CreatorAnnotation             string
	AdditionalDescriptorLayers    []ociImageSpecV1.Descriptor
	AdditionalDescriptorManifests []ociImageSpecV1.Descriptor
}

// addDescriptorToStore uploads a component descriptor to any given Store.
// The returned descriptor is the manifest descriptor of the uploaded component.
// It can be used to retrieve the component descriptor later.
// To persist the descriptor, the manifest still has to be tagged.
func addDescriptorToStore(ctx context.Context, store Store, descriptor *descriptor.Descriptor, opts storeDescriptorOptions) (*ociImageSpecV1.Descriptor, error) {
	component, version := descriptor.Component.Name, descriptor.Component.Version

	// Encode and upload the descriptor
	descriptorEncoding, descriptorBuffer, err := tar.SingleFileTAREncodeV2Descriptor(opts.Scheme, descriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to encode descriptor: %w", err)
	}
	descriptorBytes := descriptorBuffer.Bytes()
	descriptorOCIDescriptor := ociImageSpecV1.Descriptor{
		MediaType: MediaTypeComponentDescriptorV2 + descriptorEncoding,
		Digest:    digest.FromBytes(descriptorBytes),
		Size:      int64(len(descriptorBytes)),
	}
	log.Base.Log(ctx, slog.LevelDebug, "pushing descriptor", log.DescriptorLogAttr(descriptorOCIDescriptor))
	if err := store.Push(ctx, descriptorOCIDescriptor, bytes.NewReader(descriptorBytes)); err != nil {
		return nil, fmt.Errorf("unable to push component descriptor: %w", err)
	}

	// Create and upload the component configuration
	componentConfigRaw, componentConfigDescriptor, err := createComponentConfig(descriptorOCIDescriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal component config: %w", err)
	}
	log.Base.Log(ctx, slog.LevelDebug, "pushing descriptor", log.DescriptorLogAttr(componentConfigDescriptor))
	if err := store.Push(ctx, componentConfigDescriptor, bytes.NewReader(componentConfigRaw)); err != nil {
		return nil, fmt.Errorf("unable to push component config: %w", err)
	}

	// Create and upload the manifest
	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config:    componentConfigDescriptor,
		Annotations: map[string]string{
			AnnotationOCMComponentVersion: fmt.Sprintf("component-descriptors/%s:%s", component, version),
			AnnotationOCMCreator:          opts.CreatorAnnotation,
		},
		Layers: append(
			[]ociImageSpecV1.Descriptor{descriptorOCIDescriptor},
			// Add additional descriptor layers if provided
			// These are stored within the main descriptor
			opts.AdditionalDescriptorLayers...,
		),
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}
	manifestDescriptor := ociImageSpecV1.Descriptor{
		MediaType:   manifest.MediaType,
		Digest:      digest.FromBytes(manifestRaw),
		Size:        int64(len(manifestRaw)),
		Annotations: manifest.Annotations,
	}
	log.Base.Log(ctx, slog.LevelInfo, "pushing descriptor", log.DescriptorLogAttr(manifestDescriptor))
	if err := store.Push(ctx, manifestDescriptor, bytes.NewReader(manifestRaw)); err != nil {
		return nil, fmt.Errorf("unable to push manifest: %w", err)
	}

	// Only create an index if additional descriptor manifests are provided
	if len(opts.AdditionalDescriptorManifests) == 0 {
		return &manifestDescriptor, nil
	}

	idx := ociImageSpecV1.Index{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: append(
			[]ociImageSpecV1.Descriptor{manifestDescriptor},
			// Add additional descriptor manifests if provided
			// These are stored within the main index
			opts.AdditionalDescriptorManifests...,
		),
	}
	idxRaw, err := json.Marshal(idx)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal index: %w", err)
	}
	idxDescriptor := ociImageSpecV1.Descriptor{
		MediaType:   idx.MediaType,
		Digest:      digest.FromBytes(idxRaw),
		Size:        int64(len(idxRaw)),
		Annotations: idx.Annotations,
	}
	log.Base.Log(ctx, slog.LevelInfo, "pushing index", log.DescriptorLogAttr(idxDescriptor))
	if err := store.Push(ctx, idxDescriptor, bytes.NewReader(idxRaw)); err != nil {
		return nil, fmt.Errorf("unable to push index: %w", err)
	}

	return &idxDescriptor, nil
}

// getDescriptorFromStore retrieves a component descriptor from a given Store using the provided reference.
func getDescriptorFromStore(ctx context.Context, store Store, reference string) (*descriptor.Descriptor, *ociImageSpecV1.Manifest, *ociImageSpecV1.Index, error) {
	manifest, index, err := getDescriptorOCIImageManifest(ctx, store, reference)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	componentConfigRaw, err := store.Fetch(ctx, manifest.Config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get component config: %w", err)
	}
	defer func() {
		_ = componentConfigRaw.Close()
	}()
	componentConfig := ComponentConfig{}
	if err := json.NewDecoder(componentConfigRaw).Decode(&componentConfig); err != nil {
		return nil, nil, nil, err
	}

	// Read component descriptor
	descriptorRaw, err := store.Fetch(ctx, *componentConfig.ComponentDescriptorLayer)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to fetch descriptor layer: %w", err)
	}
	defer func() {
		_ = descriptorRaw.Close()
	}()

	desc, err := tar.SingleFileTARDecodeV2Descriptor(descriptorRaw)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode descriptor: %w", err)
	}

	return desc, &manifest, index, nil
}
