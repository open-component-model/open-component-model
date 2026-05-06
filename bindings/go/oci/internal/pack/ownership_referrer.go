package pack

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

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OwnershipArtifactType is the OCI artifactType set on ownership referrer
// manifests. It enables filtering via the Referrers API
// (GET /v2/<name>/referrers/<digest>?artifactType=...).
const OwnershipArtifactType = "application/vnd.ocm.software.ownership.v1+json"

func OwnershipReferrer(artifact descriptor.Artifact, component string, version string) tar.ReferrersFunc {
	return func(ctx context.Context, top ociImageSpecV1.Descriptor) ([]tar.Referrer, error) {
		if !introspection.IsOCICompliantManifest(top) {
			slog.DebugContext(ctx, "skipping ownership referrer: subject is not an OCI manifest", "mediaType", top.MediaType, "digest", top.Digest.String())
			return nil, nil
		}
		meta := artifact.GetElementMeta()
		artifactValue, err := marshalArtifactAnnotation(meta.ToIdentity(), annotations.ArtifactKindResource)
		if err != nil {
			return nil, fmt.Errorf("failed to build ownership artifact annotation: %w", err)
		}

		emptyDesc := ociImageSpecV1.DescriptorEmptyJSON
		manifest := ociImageSpecV1.Manifest{
			Versioned:    specs.Versioned{SchemaVersion: 2},
			MediaType:    ociImageSpecV1.MediaTypeImageManifest,
			ArtifactType: OwnershipArtifactType,
			Config:       emptyDesc,
			Layers:       []ociImageSpecV1.Descriptor{emptyDesc},
			Subject:      &top,
			Annotations: map[string]string{
				annotations.OwnershipComponentName:    component,
				annotations.OwnershipComponentVersion: version,
				annotations.ArtifactAnnotationKey:     artifactValue,
			},
		}
		body, err := json.Marshal(manifest)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal ownership referrer manifest: %w", err)
		}

		desc := ociImageSpecV1.Descriptor{
			MediaType:    ociImageSpecV1.MediaTypeImageManifest,
			ArtifactType: OwnershipArtifactType,
			Digest:       digest.FromBytes(body),
			Size:         int64(len(body)),
		}

		return []tar.Referrer{
			{Descriptor: desc, Raw: body},
			{Descriptor: ociImageSpecV1.DescriptorEmptyJSON, Raw: []byte("{}")},
		}, nil
	}
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
