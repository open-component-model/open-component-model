package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

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
	normalizedlayout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Layout selects the OCI storage layout for a component version.
type Layout int

const (
	// LayoutV2 is the default: the tag resolves to the access-bearing v2 descriptor manifest.
	LayoutV2 Layout = iota
	// LayoutNormalized is the cosign-signable layout: the tag resolves to a stable, access-free
	// normalized descriptor manifest, with the access-bearing descriptor stored as a referrer.
	LayoutNormalized
)

// AddDescriptorOptions defines the options for adding a component descriptor to a Store.
type AddDescriptorOptions struct {
	Scheme                        *runtime.Scheme
	Author                        string
	AdditionalDescriptorManifests []ociImageSpecV1.Descriptor
	AdditionalLayers              []ociImageSpecV1.Descriptor
	ReferrerTrackingPolicy        ReferrerTrackingPolicy
	DescriptorEncodingMediaType   string
	Layout                        Layout
}

// AddDescriptorToStore uploads a component descriptor to any given Store.
// The returned descriptor is the manifest descriptor of the uploaded component.
// It can be used to retrieve the component descriptor later.
// To persist the descriptor, the manifest still has to be tagged.
func AddDescriptorToStore(ctx context.Context, store spec.Store, descriptor *descriptor.Descriptor, opts AddDescriptorOptions) (*ociImageSpecV1.Descriptor, error) {
	if opts.Layout == LayoutNormalized {
		return addNormalizedLayout(ctx, store, descriptor, opts)
	}

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

// addNormalizedLayout uploads a component descriptor using the cosign-signable normalized layout.
//
// The tag target is a stable, access-free normalized manifest M whose single layer is the
// normalized (JCS-canonical) descriptor bytes. The full access-bearing descriptor and any
// additional local-blob layers are stored in an access manifest A that references M as its subject
// (a referrer). A is additionally tagged with a predictable fallback tag so it can be discovered on
// registries without the OCI referrers API.
//
// The returned descriptor is the normalized manifest descriptor M (untagged); the caller tags it as
// the component reference, mirroring AddDescriptorToStore.
func addNormalizedLayout(ctx context.Context, store spec.Store, desc *descriptor.Descriptor, opts AddDescriptorOptions) (*ociImageSpecV1.Descriptor, error) {
	if err := normalizedlayout.RequireAllResourcesDigested(desc); err != nil {
		return nil, err
	}

	if len(opts.AdditionalDescriptorManifests) > 0 {
		return nil, fmt.Errorf("normalized layout stores additional local blobs as manifest layers and does not support additional descriptor manifests (nested local-blob manifests)")
	}

	component, version := desc.Component.Name, desc.Component.Version

	// Build the normalized (access-free) layer that cosign signs (via the manifest digest).
	normBytes, err := normalizedlayout.Normalize(desc)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize descriptor: %w", err)
	}
	normLayer := ociImageSpecV1.Descriptor{
		MediaType: ocidescriptor.MediaTypeComponentDescriptorNormalizedJSON,
		Digest:    digest.FromBytes(normBytes),
		Size:      int64(len(normBytes)),
	}

	descriptorMediaType := opts.DescriptorEncodingMediaType
	if descriptorMediaType == "" {
		descriptorMediaType = ocidescriptor.MediaTypeComponentDescriptorJSON
	}

	// Encode the full, access-bearing descriptor as the access manifest layer.
	descBuf, err := ocidescriptor.SingleFileEncodeDescriptor(opts.Scheme, desc, descriptorMediaType)
	if err != nil {
		return nil, fmt.Errorf("failed to encode descriptor: %w", err)
	}
	descBytes := descBuf.Bytes()
	descLayer := ociImageSpecV1.Descriptor{
		MediaType: descriptorMediaType,
		Digest:    digest.FromBytes(descBytes),
		Size:      int64(len(descBytes)),
	}

	configRaw, configDesc, err := componentConfig.New(descLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal component config: %w", err)
	}

	// Push the empty config blob for the normalized manifest, tolerating pre-existence.
	emptyConfig := ociImageSpecV1.DescriptorEmptyJSON
	if exists, err := store.Exists(ctx, emptyConfig); err != nil {
		return nil, fmt.Errorf("failed to check empty config existence: %w", err)
	} else if !exists {
		if err := store.Push(ctx, emptyConfig, bytes.NewReader(emptyConfig.Data)); err != nil {
			return nil, fmt.Errorf("unable to push empty config: %w", err)
		}
	}

	// Push the content blobs.
	if err := store.Push(ctx, normLayer, bytes.NewReader(normBytes)); err != nil {
		return nil, fmt.Errorf("unable to push normalized descriptor layer: %w", err)
	}
	if err := store.Push(ctx, descLayer, bytes.NewReader(descBytes)); err != nil {
		return nil, fmt.Errorf("unable to push descriptor layer: %w", err)
	}
	if err := store.Push(ctx, configDesc, bytes.NewReader(configRaw)); err != nil {
		return nil, fmt.Errorf("unable to push component config: %w", err)
	}

	// Build and push the normalized manifest (the tag target).
	m := normalizedlayout.BuildNormalizedManifest(normLayer, component, version)
	mRaw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal normalized manifest: %w", err)
	}
	mDesc := ociImageSpecV1.Descriptor{
		MediaType:    m.MediaType,
		ArtifactType: m.ArtifactType,
		Digest:       digest.FromBytes(mRaw),
		Size:         int64(len(mRaw)),
		Annotations:  m.Annotations,
	}
	slogcontext.Log(ctx, slog.LevelDebug, "pushing normalized descriptor manifest", log.DescriptorLogAttr(mDesc))
	if err := store.Push(ctx, mDesc, bytes.NewReader(mRaw)); err != nil {
		return nil, fmt.Errorf("unable to push normalized manifest: %w", err)
	}

	// Build and push the access manifest as a referrer of the normalized manifest.
	a := normalizedlayout.BuildAccessManifest(mDesc, configDesc, descLayer, opts.AdditionalLayers, component, version)
	aRaw, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal access manifest: %w", err)
	}
	aDesc := ociImageSpecV1.Descriptor{
		MediaType:    a.MediaType,
		ArtifactType: a.ArtifactType,
		Digest:       digest.FromBytes(aRaw),
		Size:         int64(len(aRaw)),
		Annotations:  a.Annotations,
	}
	slogcontext.Log(ctx, slog.LevelDebug, "pushing access descriptor manifest", log.DescriptorLogAttr(aDesc))
	if err := store.Push(ctx, aDesc, bytes.NewReader(aRaw)); err != nil {
		return nil, fmt.Errorf("unable to push access manifest: %w", err)
	}

	// Tag the access manifest with the predictable fallback tag for referrer-less registries.
	if err := store.Tag(ctx, aDesc, normalizedlayout.AccessFallbackTag(mDesc.Digest.String())); err != nil {
		return nil, fmt.Errorf("unable to tag access manifest with fallback tag: %w", err)
	}

	return &mDesc, nil
}

// getDescriptorFromStore retrieves a component descriptor from a given Store using the provided reference.
func getDescriptorFromStore(ctx context.Context, store spec.Store, reference string, unmarshal ocidescriptor.UnmarshalFunc) (desc *descriptor.Descriptor, manifestRef *ociImageSpecV1.Manifest, index *ociImageSpecV1.Index, err error) {
	manifest, index, err := getDescriptorOCIImageManifest(ctx, store, reference)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	// This function decodes the access-bearing manifest by following its config to the component
	// descriptor layer. A normalized-layout manifest has an empty config and no descriptor layer, so
	// it cannot be decoded here; it must be read via the normalized read path instead.
	if manifest.ArtifactType == ocidescriptor.ArtifactTypeNormalizedDescriptor {
		return nil, nil, nil, fmt.Errorf("cannot decode a normalized-layout manifest here: it has no component descriptor layer; resolve it via the normalized read path")
	}

	componentConfigRaw, err := store.Fetch(ctx, manifest.Config)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get component config: %w", err)
	}
	cfg := componentConfig.Config{}
	if err := json.NewDecoder(componentConfigRaw).Decode(&cfg); err != nil {
		return nil, nil, nil, err
	}

	if closeErr := componentConfigRaw.Close(); closeErr != nil {
		return nil, nil, nil, fmt.Errorf("failed to close component config reader: %w", closeErr)
	}

	// Defensive equivalent of the check above for when ArtifactType is absent (e.g. an intermediate
	// proxy strips it): an empty config carries no descriptor layer.
	if cfg.ComponentDescriptorLayer == nil {
		return nil, nil, nil, fmt.Errorf("cannot decode a normalized-layout manifest here: it has no component descriptor layer; resolve it via the normalized read path")
	}

	// Read component descriptor
	descriptorRaw, err := store.Fetch(ctx, *cfg.ComponentDescriptorLayer)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to fetch descriptor layer: %w", err)
	}
	defer func() {
		err = errors.Join(err, descriptorRaw.Close())
	}()

	desc, err = ocidescriptor.SingleFileDecodeDescriptor(descriptorRaw, cfg.ComponentDescriptorLayer.MediaType, unmarshal)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to decode descriptor: %w", err)
	}

	return desc, &manifest, index, nil
}
