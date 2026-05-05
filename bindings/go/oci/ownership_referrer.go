package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OwnershipArtifactType is the OCI artifactType set on ownership referrer
// manifests. It enables filtering via the Referrers API
// (GET /v2/<name>/referrers/<digest>?artifactType=...).
const OwnershipArtifactType = "application/vnd.ocm.software.ownership.v1+json"

// buildOwnershipReferrer constructs an OCI referrer manifest linking subject
// to the owning component version (ADR 0015).
//
// Built by hand rather than via [oras.PackManifest] because PackManifest
// stamps org.opencontainers.image.created, breaking idempotency.
func buildOwnershipReferrer(ctx context.Context, subject ociImageSpecV1.Descriptor, resource *descriptor.Resource, component, version string) (manifestBytes []byte, manifestDesc ociImageSpecV1.Descriptor, ok bool, err error) {
	if !introspection.IsOCICompliantManifest(subject) {
		slog.DebugContext(ctx, "skipping ownership referrer: subject is not an OCI manifest", "mediaType", subject.MediaType, "digest", subject.Digest.String())
		return nil, ociImageSpecV1.Descriptor{}, false, nil
	}

	meta := resource.GetElementMeta()
	artifactValue, err := marshalArtifactAnnotation(meta.ToIdentity(), annotations.ArtifactKindResource)
	if err != nil {
		return nil, ociImageSpecV1.Descriptor{}, false, fmt.Errorf("failed to build ownership artifact annotation: %w", err)
	}

	emptyDesc := ociImageSpecV1.DescriptorEmptyJSON
	manifest := ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: OwnershipArtifactType,
		Config:       emptyDesc,
		Layers:       []ociImageSpecV1.Descriptor{emptyDesc},
		Subject:      &subject,
		Annotations: map[string]string{
			annotations.OwnershipComponentName:    component,
			annotations.OwnershipComponentVersion: version,
			annotations.ArtifactAnnotationKey:     artifactValue,
		},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		return nil, ociImageSpecV1.Descriptor{}, false, fmt.Errorf("failed to marshal ownership referrer manifest: %w", err)
	}

	desc := ociImageSpecV1.Descriptor{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: OwnershipArtifactType,
		Digest:       digest.FromBytes(body),
		Size:         int64(len(body)),
	}
	return body, desc, true, nil
}

// ownershipReferrerAttachment builds the ownership referrer for subject as
// an attachment (descriptors + source store) for the artifact upload's
// CopyGraph. Returns nil/nil when subject is not an OCI manifest — referrers
// only attach to manifests (ADR 0015).
func ownershipReferrerAttachment(
	ctx context.Context,
	subject ociImageSpecV1.Descriptor,
	resource *descriptor.Resource,
	component, version string,
) ([]ociImageSpecV1.Descriptor, content.ReadOnlyStorage, error) {
	body, desc, ok, err := buildOwnershipReferrer(ctx, subject, resource, component, version)
	if err != nil || !ok {
		return nil, nil, err
	}

	emptyDesc := ociImageSpecV1.DescriptorEmptyJSON
	src := memory.New()
	if err := src.Push(ctx, emptyDesc, bytes.NewReader(emptyDesc.Data)); err != nil {
		return nil, nil, fmt.Errorf("failed to stage empty config/layer blob: %w", err)
	}
	if err := src.Push(ctx, desc, bytes.NewReader(body)); err != nil {
		return nil, nil, fmt.Errorf("failed to stage ownership referrer manifest: %w", err)
	}
	return []ociImageSpecV1.Descriptor{desc}, src, nil
}

// marshalArtifactAnnotation serialises the {identity, kind} value stored under
// [annotations.ArtifactAnnotationKey] on an ownership referrer (ADR 0015).
// Output is JCS-canonical (RFC 8785) for this schema: encoding/json sorts
// map[string]string keys, the struct fields are already alphabetical, leaves
// are strings (no number-formatting concerns), and HTML escaping is explicitly
// disabled so '<', '>', '&', U+2028 and U+2029 are emitted verbatim rather
// than \u-encoded — which RFC 8785 requires but encoding/json does not do by
// default.
func marshalArtifactAnnotation(identity runtime.Identity, kind annotations.ArtifactKind) (string, error) {
	payload := struct {
		Identity runtime.Identity         `json:"identity"`
		Kind     annotations.ArtifactKind `json:"kind"`
	}{Identity: identity, Kind: kind}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return "", fmt.Errorf("failed to marshal artifact annotation: %w", err)
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
