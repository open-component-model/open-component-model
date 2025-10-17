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
	slogcontext "github.com/veqryn/slog-context"
	"golang.org/x/sync/errgroup"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/log"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	indexv1 "ocm.software/open-component-model/bindings/go/oci/spec/index/component/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// AddDescriptorOptions defines the options for adding a component descriptor to a Store.
type AddDescriptorOptions struct {
	Scheme                        *runtime.Scheme
	Author                        string
	AdditionalDescriptorManifests []ociImageSpecV1.Descriptor
	AdditionalLayers              []ociImageSpecV1.Descriptor
	ReferrerTrackingPolicy        ReferrerTrackingPolicy
	DescriptorEncodingMediaType   string
}

// AddDescriptorToStore uploads a component descriptor to any given Store.
// The returned descriptor is the manifest descriptor of the uploaded component.
// It can be used to retrieve the component descriptor later.
// To persist the descriptor, the manifest still has to be tagged.
func AddDescriptorToStore(ctx context.Context, store spec.Store, descriptor *descriptor.Descriptor, opts AddDescriptorOptions) (*ociImageSpecV1.Descriptor, error) {
	component, version := descriptor.Component.Name, descriptor.Component.Version

	// we can concurrently upload certain parts of the descriptor!
	eg, egctx := errgroup.WithContext(ctx)

	if opts.ReferrerTrackingPolicy == ReferrerTrackingPolicyByIndexAndSubject {
		eg.Go(func() error {
			if err := indexv1.CreateIfNotExists(egctx, store); err != nil {
				return fmt.Errorf("failed to create index: %w", err)
			}
			return nil
		})
	}

	descriptorMediaType := opts.DescriptorEncodingMediaType
	if descriptorMediaType == "" {
		// Default to JSON if no media type is provided, as this is the defacto canonical standard format
		// used when integrating with OCI usually.
		descriptorMediaType = ocidescriptor.MediaTypeComponentDescriptorJSON
	}

	// Encode and upload the descriptor
	descriptorBuffer, err := ocidescriptor.SingleFileEncodeDescriptor(opts.Scheme, descriptor, descriptorMediaType)
	if err != nil {
		return nil, fmt.Errorf("failed to encode descriptor: %w", err)
	}

	descriptorBytes := descriptorBuffer.Bytes()
	descriptorOCIDescriptor := ociImageSpecV1.Descriptor{
		MediaType: descriptorMediaType,
		Digest:    digest.FromBytes(descriptorBytes),
		Size:      int64(len(descriptorBytes)),
	}

	eg.Go(func() error {
		slogcontext.Log(egctx, slog.LevelDebug, "pushing component descriptor", log.DescriptorLogAttr(descriptorOCIDescriptor))
		if err := store.Push(egctx, descriptorOCIDescriptor, bytes.NewReader(descriptorBytes)); err != nil {
			return fmt.Errorf("unable to push component descriptor: %w", err)
		}
		return nil
	})

	// New and upload the component configuration
	componentConfigRaw, componentConfigDescriptor, err := componentConfig.New(descriptorOCIDescriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal component config: %w", err)
	}

	eg.Go(func() error {
		slogcontext.Log(egctx, slog.LevelDebug, "pushing component config", log.DescriptorLogAttr(componentConfigDescriptor))
		if err := store.Push(egctx, componentConfigDescriptor, bytes.NewReader(componentConfigRaw)); err != nil {
			return fmt.Errorf("unable to push component config: %w", err)
		}
		return nil
	})

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("failed to push meta layers for descriptor %s: %w", descriptor, err)
	}

	// New and upload the manifest
	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		ArtifactType: ocidescriptor.MediaTypeComponentDescriptorV2,
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		Config:       componentConfigDescriptor,
		Annotations: map[string]string{
			annotations.OCMComponentVersion: annotations.NewComponentVersionAnnotation(component, version),
			annotations.OCMCreator:          opts.Author,
			ociImageSpecV1.AnnotationTitle:  fmt.Sprintf("OCM Component Descriptor OCI Artifact Manifest for %s in version %s", component, version),
			ociImageSpecV1.AnnotationDescription: fmt.Sprintf(`
This is an OCM OCI Artifact Manifest that contains the component descriptor for the component %[1]s.
It is used to store the component descriptor in an OCI registry and can be referrenced by the official OCM Binding Library.
`, component),
			ociImageSpecV1.AnnotationAuthors:       opts.Author,
			ociImageSpecV1.AnnotationURL:           "https://ocm.software",
			ociImageSpecV1.AnnotationDocumentation: "https://ocm.software",
			ociImageSpecV1.AnnotationSource:        "https://github.com/open-component-model/open-component-model",
			ociImageSpecV1.AnnotationVersion:       version,
		},
		Layers: append([]ociImageSpecV1.Descriptor{descriptorOCIDescriptor}, opts.AdditionalLayers...),
	}
	if opts.ReferrerTrackingPolicy == ReferrerTrackingPolicyByIndexAndSubject {
		manifest.Subject = &indexv1.Descriptor
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}
	manifestDescriptor := ociImageSpecV1.Descriptor{
		MediaType:    manifest.MediaType,
		ArtifactType: manifest.ArtifactType,
		Digest:       digest.FromBytes(manifestRaw),
		Size:         int64(len(manifestRaw)),
		Annotations:  manifest.Annotations,
	}
	slogcontext.Log(ctx, slog.LevelDebug, "pushing descriptor artifact manifest", log.DescriptorLogAttr(manifestDescriptor))
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
		Annotations: map[string]string{
			annotations.OCMComponentVersion: annotations.NewComponentVersionAnnotation(component, version),
			annotations.OCMCreator:          opts.Author,
			ociImageSpecV1.AnnotationTitle:  fmt.Sprintf("OCM Component Descriptor OCI Artifact Manifest Index for %s in version %s", component, version),
			ociImageSpecV1.AnnotationDescription: fmt.Sprintf(`
This is an OCM OCI Artifact Manifest Index that contains the component descriptor manifest for the component %[1]s.
It is used to store the component descriptor manifest and other related blob manifests in an OCI registry and can be referrenced by the official OCM Binding Library.
`, component),
			ociImageSpecV1.AnnotationAuthors:       opts.Author,
			ociImageSpecV1.AnnotationURL:           "https://ocm.software",
			ociImageSpecV1.AnnotationDocumentation: "https://ocm.software",
			ociImageSpecV1.AnnotationSource:        "https://github.com/open-component-model/open-component-model",
			ociImageSpecV1.AnnotationVersion:       version,
		},
	}
	if opts.ReferrerTrackingPolicy == ReferrerTrackingPolicyByIndexAndSubject {
		idx.Subject = &indexv1.Descriptor
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
	slogcontext.Log(ctx, slog.LevelInfo, "pushing descriptor artifact image index", log.DescriptorLogAttr(idxDescriptor))
	if err := store.Push(ctx, idxDescriptor, bytes.NewReader(idxRaw)); err != nil {
		return nil, fmt.Errorf("unable to push index: %w", err)
	}

	return &idxDescriptor, nil
}

// getDescriptorFromStore retrieves a component descriptor from a given Store using the provided reference.
func getDescriptorFromStore(ctx context.Context, store spec.Store, reference string) (*descriptor.Descriptor, *ociImageSpecV1.Manifest, *ociImageSpecV1.Index, error) {
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
	cfg := componentConfig.Config{}
	if err := json.NewDecoder(componentConfigRaw).Decode(&cfg); err != nil {
		return nil, nil, nil, err
	}

	// Read component descriptor
	descriptorRaw, err := store.Fetch(ctx, *cfg.ComponentDescriptorLayer)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to fetch descriptor layer: %w", err)
	}
	defer func() {
		_ = descriptorRaw.Close()
	}()

	desc, err := ocidescriptor.SingleFileDecodeDescriptor(descriptorRaw, cfg.ComponentDescriptorLayer.MediaType)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode descriptor: %w", err)
	}

	return desc, &manifest, index, nil
}
