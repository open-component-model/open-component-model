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
	speclayout "ocm.software/open-component-model/bindings/go/oci/spec/layout"
	normalizedlayout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Layout selects the OCI storage layout for a component version.
type Layout int

const (
	// LayoutAuto keeps the layout of the component version already stored under the same tag and
	// uses LayoutV2 for new versions, so that rewriting a version (e.g. persisting a signature)
	// never switches its layout implicitly. Store-level APIs without a tag treat it as LayoutV2.
	LayoutAuto Layout = iota
	// LayoutV2: the tag resolves to the access-bearing v2 descriptor manifest.
	LayoutV2
	// LayoutNormalized is the cosign-signable layout: the tag resolves to a stable, access-free
	// normalized descriptor manifest, with the access-bearing descriptor stored as a referrer.
	LayoutNormalized
)

// String returns the layout name used in repository specifications.
func (l Layout) String() string {
	switch l {
	case LayoutAuto:
		return "auto"
	case LayoutV2:
		return string(speclayout.ComponentVersionLayoutV2)
	case LayoutNormalized:
		return string(speclayout.ComponentVersionLayoutNormalized)
	default:
		return fmt.Sprintf("Layout(%d)", int(l))
	}
}

// LayoutFromSpec maps the layout of a repository specification to a Layout. The empty value maps
// to LayoutAuto; unknown values are rejected.
func LayoutFromSpec(l speclayout.ComponentVersionLayout) (Layout, error) {
	switch l {
	case "":
		return LayoutAuto, nil
	case speclayout.ComponentVersionLayoutV2:
		return LayoutV2, nil
	case speclayout.ComponentVersionLayoutNormalized:
		return LayoutNormalized, nil
	default:
		return LayoutAuto, l.Validate()
	}
}

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
// normalized (jsonNormalisation/v4alpha1) descriptor bytes. The full access-bearing descriptor and
// the local blobs are stored in an access referrer A whose subject is M: an image manifest listing
// the descriptor and local-blob layers, or, if OCI-native local blobs exist, an image index whose
// first manifest is that access manifest followed by the local-blob manifests (as in the v2 index).
// A is also tagged with the fallback tag derived from M, which identifies the access referrer
// written last by OCM.
//
// The returned descriptor is the normalized manifest descriptor M (untagged); the caller tags it as
// the component reference, mirroring AddDescriptorToStore.
func addNormalizedLayout(ctx context.Context, store spec.Store, desc *descriptor.Descriptor, opts AddDescriptorOptions) (*ociImageSpecV1.Descriptor, error) {
	if err := normalizedlayout.ValidateDigests(desc); err != nil {
		return nil, err
	}

	component, version := desc.Component.Name, desc.Component.Version

	var subject *ociImageSpecV1.Descriptor
	if opts.ReferrerTrackingPolicy == ReferrerTrackingPolicyByIndexAndSubject {
		if err := indexv1.CreateIfNotExists(ctx, store); err != nil {
			return nil, fmt.Errorf("failed to create index: %w", err)
		}
		subject = &indexv1.Descriptor
	}

	normBytes, err := normalizedlayout.Normalize(desc)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize descriptor: %w", err)
	}
	normLayer := ociImageSpecV1.Descriptor{
		MediaType: ocidescriptor.MediaTypeComponentDescriptorNormalizedJSON,
		Digest:    digest.FromBytes(normBytes),
		Size:      int64(len(normBytes)),
	}
	normConfigRaw, normConfigDesc, err := normalizedlayout.NewNormalizedConfig(normLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal normalized config: %w", err)
	}

	descriptorMediaType := opts.DescriptorEncodingMediaType
	if descriptorMediaType == "" {
		descriptorMediaType = ocidescriptor.MediaTypeComponentDescriptorJSON
	}
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

	// Re-adding a version (e.g. to persist an OCM signature) reproduces M and its blobs, so all
	// pushes must tolerate existing content.
	for _, b := range []struct {
		desc ociImageSpecV1.Descriptor
		raw  []byte
	}{
		{normLayer, normBytes},
		{normConfigDesc, normConfigRaw},
		{descLayer, descBytes},
		{configDesc, configRaw},
	} {
		if err := pushIfNotExists(ctx, store, b.desc, b.raw); err != nil {
			return nil, err
		}
	}

	m := normalizedlayout.BuildNormalizedManifest(normLayer, normConfigDesc, subject, component, version)
	mDesc, err := pushJSONManifest(ctx, store, m, m.MediaType, m.ArtifactType, m.Annotations)
	if err != nil {
		return nil, fmt.Errorf("unable to push normalized manifest: %w", err)
	}
	accessSubject := ociImageSpecV1.Descriptor{MediaType: mDesc.MediaType, Digest: mDesc.Digest, Size: mDesc.Size}

	var aDesc ociImageSpecV1.Descriptor
	if len(opts.AdditionalDescriptorManifests) == 0 {
		a := normalizedlayout.BuildAccessManifest(&accessSubject, configDesc, descLayer, opts.AdditionalLayers, component, version)
		if aDesc, err = pushJSONManifest(ctx, store, a, a.MediaType, a.ArtifactType, a.Annotations); err != nil {
			return nil, fmt.Errorf("unable to push access manifest: %w", err)
		}
	} else {
		inner := normalizedlayout.BuildAccessManifest(nil, configDesc, descLayer, opts.AdditionalLayers, component, version)
		innerDesc, err := pushJSONManifest(ctx, store, inner, inner.MediaType, inner.ArtifactType, nil)
		if err != nil {
			return nil, fmt.Errorf("unable to push access manifest: %w", err)
		}
		idx := normalizedlayout.BuildAccessIndex(accessSubject, innerDesc, opts.AdditionalDescriptorManifests, component, version)
		if aDesc, err = pushJSONManifest(ctx, store, idx, idx.MediaType, idx.ArtifactType, idx.Annotations); err != nil {
			return nil, fmt.Errorf("unable to push access index: %w", err)
		}
	}

	if err := store.Tag(ctx, aDesc, normalizedlayout.AccessFallbackTag(mDesc.Digest.String())); err != nil {
		return nil, fmt.Errorf("unable to tag access referrer with fallback tag: %w", err)
	}

	return &mDesc, nil
}

// pushJSONManifest marshals a manifest or index and pushes it unless it already exists.
func pushJSONManifest(ctx context.Context, store spec.Store, v any, mediaType, artifactType string, annos map[string]string) (ociImageSpecV1.Descriptor, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to marshal %s: %w", mediaType, err)
	}
	desc := ociImageSpecV1.Descriptor{
		MediaType:    mediaType,
		ArtifactType: artifactType,
		Digest:       digest.FromBytes(raw),
		Size:         int64(len(raw)),
		Annotations:  annos,
	}
	slogcontext.Log(ctx, slog.LevelDebug, "pushing normalized layout manifest", log.DescriptorLogAttr(desc))
	if err := pushIfNotExists(ctx, store, desc, raw); err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	return desc, nil
}

func pushIfNotExists(ctx context.Context, store spec.Store, desc ociImageSpecV1.Descriptor, raw []byte) error {
	exists, err := store.Exists(ctx, desc)
	if err != nil {
		return fmt.Errorf("failed to check existence of %s: %w", desc.Digest, err)
	}
	if exists {
		return nil
	}
	if err := store.Push(ctx, desc, bytes.NewReader(raw)); err != nil {
		return fmt.Errorf("unable to push %s: %w", desc.Digest, err)
	}
	return nil
}

// layoutOf reports the layout of a tagged component version from its fetched manifest.
// Registries do not populate the resolved descriptor's artifactType for tags; it is only known
// from the manifest body.
func layoutOf(manifest *ociImageSpecV1.Manifest, index *ociImageSpecV1.Index) Layout {
	if index == nil && manifest.ArtifactType == ocidescriptor.ArtifactTypeNormalizedDescriptor {
		return LayoutNormalized
	}
	return LayoutV2
}

// getStoredLayout reports the layout of the component version tagged with reference. Errors
// resolving the tag are returned unwrapped from the store, so callers can detect absence via
// errdef.ErrNotFound.
func getStoredLayout(ctx context.Context, store spec.Store, reference string) (Layout, error) {
	base, err := store.Resolve(ctx, reference)
	if err != nil {
		return LayoutAuto, err
	}
	manifest, index, err := getDescriptorOCIImageManifest(ctx, store, base)
	if err != nil {
		return LayoutAuto, fmt.Errorf("failed to get manifest: %w", err)
	}
	return layoutOf(&manifest, index), nil
}

// getDescriptorFromStore resolves reference once and retrieves the component descriptor for either
// storage layout, together with the manifest (and index) its local blobs are listed in.
func getDescriptorFromStore(ctx context.Context, store spec.Store, reference string, unmarshal ocidescriptor.UnmarshalFunc) (_ *storedComponentVersion, err error) {
	slogcontext.Log(ctx, slog.LevelDebug, "resolving descriptor", slog.String("reference", reference))
	base, err := store.Resolve(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve reference %q: %w", reference, err)
	}
	manifest, index, err := getDescriptorOCIImageManifest(ctx, store, base)
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest: %w", err)
	}

	if layoutOf(&manifest, index) == LayoutNormalized {
		return getNormalizedComponentVersion(ctx, store, base, &manifest, unmarshal)
	}

	slogcontext.Log(ctx, slog.LevelDebug, "fetching descriptor manifest config", log.DescriptorLogAttr(manifest.Config))
	componentConfigRaw, err := fetchVerified(ctx, store, manifest.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to get component config: %w", err)
	}
	cfg := componentConfig.Config{}
	if err := json.Unmarshal(componentConfigRaw, &cfg); err != nil {
		return nil, err
	}
	if cfg.ComponentDescriptorLayer == nil {
		return nil, fmt.Errorf("artifact %q is not an OCM component version: its config %s has no componentDescriptorLayer", reference, manifest.Config.Digest)
	}

	// Read component descriptor
	descriptorLayer := *cfg.ComponentDescriptorLayer
	slogcontext.Log(ctx, slog.LevelDebug, "fetching component descriptor layer", log.DescriptorLogAttr(descriptorLayer))
	descriptorRaw, err := fetchVerified(ctx, store, descriptorLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch descriptor layer: %w", err)
	}

	desc, err := ocidescriptor.SingleFileDecodeDescriptor(bytes.NewReader(descriptorRaw), descriptorLayer.MediaType, unmarshal)
	if err != nil {
		return nil, fmt.Errorf("failed to decode descriptor: %w", err)
	}

	return &storedComponentVersion{descriptor: desc, manifest: &manifest, index: index, layout: LayoutV2}, nil
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
