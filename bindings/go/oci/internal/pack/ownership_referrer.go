package pack

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OwnershipReferrer returns a [tar.ReferrersFunc] linking the packed
// artifact to its owning component version (ADR 0016). Subjects that
// are not OCI manifests are skipped with a debug log.
func OwnershipReferrer(artifact descriptor.Artifact, component string, version string) tar.ReferrersFunc {
	return func(ctx context.Context, top ociImageSpecV1.Descriptor) ([]tar.Referrer, error) {
		if !introspection.IsOCICompliantManifest(top) {
			slog.DebugContext(ctx, "skipping ownership referrer: subject is not an OCI manifest", "mediaType", top.MediaType, "digest", top.Digest.String())
			return nil, nil
		}

		kind, err := artifactKind(artifact)
		if err != nil {
			return nil, err
		}
		meta := artifact.GetElementMeta()
		artifactValue, err := marshalArtifactAnnotation(meta.ToIdentity(), kind)
		if err != nil {
			return nil, fmt.Errorf("failed to build ownership artifact annotation: %w", err)
		}

		emptyDesc := ociImageSpecV1.DescriptorEmptyJSON
		manifest := ociImageSpecV1.Manifest{
			Versioned:    specs.Versioned{SchemaVersion: 2},
			MediaType:    ociImageSpecV1.MediaTypeImageManifest,
			ArtifactType: annotations.OwnershipArtifactType,
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
			ArtifactType: annotations.OwnershipArtifactType,
			Digest:       digest.FromBytes(body),
			Size:         int64(len(body)),
		}

		return []tar.Referrer{
			{Descriptor: desc, Raw: body},
			{Descriptor: ociImageSpecV1.DescriptorEmptyJSON, Raw: ociImageSpecV1.DescriptorEmptyJSON.Data},
		}, nil
	}
}

// artifactKind reports the [annotations.ArtifactKind] for the given artifact.
func artifactKind(artifact descriptor.Artifact) (annotations.ArtifactKind, error) {
	if _, ok := artifact.(*descriptor.Resource); !ok {
		return "", fmt.Errorf("unsupported artifact type: %T", artifact)
	}
	return annotations.ArtifactKindResource, nil
}

// marshalArtifactAnnotation serialises the {identity, kind} value stored under
// [annotations.ArtifactAnnotationKey] on an ownership referrer (ADR 0016).
// The result is JCS-canonical (RFC 8785).
func marshalArtifactAnnotation(identity runtime.Identity, kind annotations.ArtifactKind) (string, error) {
	payload := struct {
		Identity runtime.Identity         `json:"identity"`
		Kind     annotations.ArtifactKind `json:"kind"`
	}{Identity: identity, Kind: kind}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal artifact annotation: %w", err)
	}
	canonical, err := jsoncanonicalizer.Transform(raw)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize artifact annotation: %w", err)
	}
	return string(canonical), nil
}
