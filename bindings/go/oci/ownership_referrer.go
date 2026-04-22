package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"

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

// pushOwnershipReferrer pushes an OCI referrer manifest linking the subject
// resource to the owning component version, per
// docs/adr/0015_ownership_annotations.md.
//
// The referrer is a minimal OCI 1.1 manifest with empty config and layer,
// an artifactType of OwnershipArtifactType, and annotations carrying the
// component name, version, and the resource identity.
//
// Ownership referrers are only pushed for resources; sources are intentionally
// not covered by this feature (ADR 0015 asset-to-owner scenario scopes
// ownership discovery to consumed OCI assets, which are resources).
//
// The call is a no-op when the subject is not an OCI-compliant manifest
// (e.g. a raw blob layer), since the OCI subject field requires a manifest
// target. Registries that support the OCI Distribution v1.1 Referrers API
// index the manifest via its subject field; ORAS falls back to the referrers
// tag schema for older registries.
func pushOwnershipReferrer(ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor, resource *descriptor.Resource, component, version string) error {
	if !introspection.IsOCICompliantManifest(subject) {
		return nil
	}

	meta := resource.GetElementMeta()
	artifactValue, err := marshalArtifactAnnotation(meta.ToIdentity(), annotations.ArtifactKindResource)
	if err != nil {
		return fmt.Errorf("failed to build ownership artifact annotation: %w", err)
	}

	opts := oras.PackManifestOptions{
		Subject: &subject,
		ManifestAnnotations: map[string]string{
			annotations.OwnershipComponentName:    component,
			annotations.OwnershipComponentVersion: version,
			annotations.ArtifactAnnotationKey:     artifactValue,
		},
	}

	if _, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, OwnershipArtifactType, opts); err != nil {
		return fmt.Errorf("failed to pack ownership referrer manifest: %w", err)
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
