package oci

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/log"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	indexv1 "ocm.software/open-component-model/bindings/go/oci/spec/index/component/v1"
	normalizedlayout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// addNormalizedLayout uploads a component descriptor using the cosign-signable normalized layout.
//
// The tag target is a stable, access-free normalized manifest M whose single layer is the
// normalized (jsonNormalisation/v4alpha1) descriptor bytes. The full access-bearing descriptor and
// the local blobs are stored in an access referrer A whose subject is M: an image manifest listing
// the descriptor and local-blob layers, or, if OCI-native local blobs exist, an image index whose
// first manifest is that access manifest followed by the local-blob manifests (as in the v2 index).
// A carries no tag of its own and is discovered on read as a referrer of M. It is stamped with an
// org.opencontainers.image.created timestamp so the reader can pick the most recently written one
// when several valid access referrers of the same M exist (e.g. after re-adding the version to
// persist a signature, or after a third-party push).
//
// The returned descriptor is the normalized manifest descriptor M (untagged); the caller tags it as
// the component reference, mirroring AddDescriptorToStore.
func addNormalizedLayout(ctx context.Context, store spec.Store, desc *descriptor.Descriptor, opts AddDescriptorOptions) (*ociImageSpecV1.Descriptor, error) {
	if err := normalizedlayout.ValidateDigests(desc); err != nil {
		return nil, err
	}
	mDesc, err := pushNormalizedManifest(ctx, store, desc, opts)
	if err != nil {
		return nil, err
	}
	if err := pushAccessReferrer(ctx, store, mDesc, desc, opts); err != nil {
		return nil, err
	}
	return &mDesc, nil
}

// pushNormalizedManifest builds the normalized manifest M of desc and pushes it with its config
// and its layer, the normalized descriptor. M depends only on the normalized descriptor (and the
// referrer tracking policy), so re-adding a version (e.g. to persist an OCM signature) reproduces
// it and its blobs: all pushes tolerate existing content.
func pushNormalizedManifest(ctx context.Context, store spec.Store, desc *descriptor.Descriptor, opts AddDescriptorOptions) (ociImageSpecV1.Descriptor, error) {
	var subject *ociImageSpecV1.Descriptor
	if opts.ReferrerTrackingPolicy == ReferrerTrackingPolicyByIndexAndSubject {
		if err := indexv1.CreateIfNotExists(ctx, store); err != nil {
			return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to create index: %w", err)
		}
		subject = &indexv1.Descriptor
	}

	normBytes, err := normalizedlayout.Normalize(desc)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to normalize descriptor: %w", err)
	}
	normLayer := ociImageSpecV1.Descriptor{
		MediaType: ocidescriptor.MediaTypeComponentDescriptorNormalizedJSON,
		Digest:    digest.FromBytes(normBytes),
		Size:      int64(len(normBytes)),
	}
	normConfigRaw, normConfigDesc, err := normalizedlayout.NewNormalizedConfig(normLayer)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("failed to marshal normalized config: %w", err)
	}
	if err := pushIfNotExists(ctx, store, normLayer, normBytes); err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}
	if err := pushIfNotExists(ctx, store, normConfigDesc, normConfigRaw); err != nil {
		return ociImageSpecV1.Descriptor{}, err
	}

	m := normalizedlayout.BuildNormalizedManifest(normLayer, normConfigDesc, subject, desc.Component.Name, desc.Component.Version)
	mDesc, err := pushJSONManifest(ctx, store, m, m.MediaType, m.ArtifactType, m.Annotations)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, fmt.Errorf("unable to push normalized manifest: %w", err)
	}
	return mDesc, nil
}

// pushAccessReferrer pushes the access referrer A of the normalized manifest: the full,
// access-bearing desc with its config, listed together with the local blobs of opts in an image
// manifest or, if OCI-native local blobs exist, in an image index. The subject of A is normalized.
func pushAccessReferrer(ctx context.Context, store spec.Store, normalized ociImageSpecV1.Descriptor, desc *descriptor.Descriptor, opts AddDescriptorOptions) error {
	component, version := desc.Component.Name, desc.Component.Version

	descriptorMediaType := opts.DescriptorEncodingMediaType
	if descriptorMediaType == "" {
		descriptorMediaType = ocidescriptor.MediaTypeComponentDescriptorJSON
	}
	descBuf, err := ocidescriptor.SingleFileEncodeDescriptor(opts.Scheme, desc, descriptorMediaType)
	if err != nil {
		return fmt.Errorf("failed to encode descriptor: %w", err)
	}
	descBytes := descBuf.Bytes()
	descLayer := ociImageSpecV1.Descriptor{
		MediaType: descriptorMediaType,
		Digest:    digest.FromBytes(descBytes),
		Size:      int64(len(descBytes)),
	}
	configRaw, configDesc, err := componentConfig.New(descLayer)
	if err != nil {
		return fmt.Errorf("failed to marshal component config: %w", err)
	}
	if err := pushIfNotExists(ctx, store, descLayer, descBytes); err != nil {
		return err
	}
	if err := pushIfNotExists(ctx, store, configDesc, configRaw); err != nil {
		return err
	}

	subject := ociImageSpecV1.Descriptor{MediaType: normalized.MediaType, Digest: normalized.Digest, Size: normalized.Size}

	// Stamp the access referrer with its creation time so a later writer's referrer is preferred on
	// read. The timestamp makes A content-unique per write, which is intended: unlike M it is never
	// signed and is regenerated per repository.
	created := time.Now().UTC().Format(time.RFC3339Nano)
	if len(opts.AdditionalDescriptorManifests) == 0 {
		a := normalizedlayout.BuildAccessManifest(&subject, configDesc, descLayer, opts.AdditionalLayers, component, version)
		a.Annotations[ociImageSpecV1.AnnotationCreated] = created
		if _, err := pushJSONManifest(ctx, store, a, a.MediaType, a.ArtifactType, a.Annotations); err != nil {
			return fmt.Errorf("unable to push access manifest: %w", err)
		}
		return nil
	}

	inner := normalizedlayout.BuildAccessManifest(nil, configDesc, descLayer, opts.AdditionalLayers, component, version)
	innerDesc, err := pushJSONManifest(ctx, store, inner, inner.MediaType, inner.ArtifactType, nil)
	if err != nil {
		return fmt.Errorf("unable to push access manifest: %w", err)
	}
	idx := normalizedlayout.BuildAccessIndex(subject, innerDesc, opts.AdditionalDescriptorManifests, component, version)
	idx.Annotations[ociImageSpecV1.AnnotationCreated] = created
	if _, err := pushJSONManifest(ctx, store, idx, idx.MediaType, idx.ArtifactType, idx.Annotations); err != nil {
		return fmt.Errorf("unable to push access index: %w", err)
	}
	return nil
}

// pushJSONManifest marshals a manifest or index and pushes it unless it already exists.
func pushJSONManifest(ctx context.Context, store spec.Store, v any, mediaType, artifactType string, annos map[string]string) (ociImageSpecV1.Descriptor, error) {
	raw, err := json.Marshal(v, runtime.JSONOptions)
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
