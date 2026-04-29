package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/errdef"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OwnershipArtifactType is the OCI artifactType set on ownership referrer
// manifests. It enables filtering via the Referrers API
// (GET /v2/<name>/referrers/<digest>?artifactType=...).
const OwnershipArtifactType = "application/vnd.ocm.software.ownership.v1+json"

// pushOwnershipReferrer pushes an OCI referrer manifest linking subject to
// the owning component version, per docs/adr/0015_ownership_annotations.md.
//
// The manifest is built by hand rather than via [oras.PackManifest] because
// PackManifest unconditionally stamps org.opencontainers.image.created (caller
// value or time.Now()), which would make the digest non-deterministic.
// Omitting that annotation is spec-legal — created is optional in the OCI
// image-spec — so identical inputs produce identical bytes and re-running
// `ocm add cv` is a no-op at the registry instead of a new referrer per run.
func pushOwnershipReferrer(ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor, resource *descriptor.Resource, component, version string) error {
	if !introspection.IsOCICompliantManifest(subject) {
		return nil
	}

	meta := resource.GetElementMeta()
	artifactValue, err := marshalArtifactAnnotation(meta.ToIdentity(), annotations.ArtifactKindResource)
	if err != nil {
		return fmt.Errorf("failed to build ownership artifact annotation: %w", err)
	}

	// Both the config and the single layer point at the standard empty-JSON
	// blob ({}). One Push covers both fields.
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
		return fmt.Errorf("failed to marshal ownership referrer manifest: %w", err)
	}

	// Push the empty config/layer blob. errdef.ErrAlreadyExists is the normal
	// signal that the content is already present — expected on re-runs and
	// across referrers that share the same empty blob.
	if err := store.Push(ctx, emptyDesc, bytes.NewReader(emptyDesc.Data)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return fmt.Errorf("failed to push empty config/layer blob for ownership referrer: %w", err)
	}

	manifestDesc := ociImageSpecV1.Descriptor{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: OwnershipArtifactType,
		Digest:       digest.FromBytes(body),
		Size:         int64(len(body)),
	}
	if err := store.Push(ctx, manifestDesc, bytes.NewReader(body)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		return fmt.Errorf("failed to push ownership referrer manifest: %w", err)
	}

	return nil
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
