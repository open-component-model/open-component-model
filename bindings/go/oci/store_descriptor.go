package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"
	"golang.org/x/sync/errgroup"
	"oras.land/oras-go/v2/content"

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
			ociImageSpecV1.AnnotationDescription: strings.TrimSpace(fmt.Sprintf(`
This is an OCM OCI Artifact Manifest that contains the component descriptor for the component %[1]s.
It is used to store the component descriptor in an OCI registry and can be referrenced by the official OCM Binding Library.
`, component)),
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
			ociImageSpecV1.AnnotationDescription: strings.TrimSpace(fmt.Sprintf(`
This is an OCM OCI Artifact Manifest Index that contains the component descriptor manifest for the component %[1]s.
It is used to store the component descriptor manifest and other related blob manifests in an OCI registry and can be referrenced by the official OCM Binding Library.
`, component)),
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
// Every node from the artifact reference resolves to down to the descriptor layer is verified
// against its digest, so the descriptor is bound to the digest of that artifact.
func getDescriptorFromStore(ctx context.Context, store spec.Store, reference string, unmarshal ocidescriptor.UnmarshalFunc) (desc *descriptor.Descriptor, manifestRef *ociImageSpecV1.Manifest, index *ociImageSpecV1.Index, err error) {
	manifest, index, err := getDescriptorOCIImageManifest(ctx, store, reference)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	slogcontext.Log(ctx, slog.LevelDebug, "fetching descriptor manifest config", log.DescriptorLogAttr(manifest.Config))
	componentConfigRaw, err := fetchVerified(ctx, store, manifest.Config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get component config: %w", err)
	}
	cfg := componentConfig.Config{}
	if err := json.Unmarshal(componentConfigRaw, &cfg); err != nil {
		return nil, nil, nil, err
	}
	if cfg.ComponentDescriptorLayer == nil {
		return nil, nil, nil, fmt.Errorf("artifact %q is not an OCM component version: its config %s has no componentDescriptorLayer", reference, manifest.Config.Digest)
	}

	// Read component descriptor
	descriptorLayer := *cfg.ComponentDescriptorLayer
	slogcontext.Log(ctx, slog.LevelDebug, "fetching component descriptor layer", log.DescriptorLogAttr(descriptorLayer))
	descriptorRaw, err := fetchVerified(ctx, store, descriptorLayer)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to fetch descriptor layer: %w", err)
	}

	desc, err = ocidescriptor.SingleFileDecodeDescriptor(bytes.NewReader(descriptorRaw), descriptorLayer.MediaType, unmarshal)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode descriptor: %w", err)
	}

	return desc, &manifest, index, nil
}

// fetchVerified reads desc from fetcher and checks the content against the digest of desc (and its
// size, if known), so that an artifact whose digest is trusted vouches for everything read through it.
func fetchVerified(ctx context.Context, fetcher content.Fetcher, desc ociImageSpecV1.Descriptor) ([]byte, error) {
	if err := desc.Digest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid digest %q of %s: %w", desc.Digest, desc.MediaType, err)
	}
	rc, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	var r io.Reader = rc
	if desc.Size > 0 {
		// one byte more than expected detects oversized content without reading all of it
		r = io.LimitReader(rc, desc.Size+1)
	}
	data, err := io.ReadAll(r)
	if err = errors.Join(err, rc.Close()); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", desc.Digest, err)
	}
	if desc.Size > 0 && int64(len(data)) != desc.Size {
		return nil, fmt.Errorf("size mismatch for %s: expected %d bytes, got %d", desc.Digest, desc.Size, len(data))
	}
	if actual := desc.Digest.Algorithm().FromBytes(data); actual != desc.Digest {
		return nil, fmt.Errorf("digest mismatch: expected %s, got %s", desc.Digest, actual)
	}
	return data, nil
}
