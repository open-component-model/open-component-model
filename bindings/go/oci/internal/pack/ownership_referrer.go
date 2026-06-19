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
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OwnershipReferrer builds an ownership referrer - an OCI manifest with a
// subject pointing at the OCI resource with annotations containing ownership
// information (i.e. component name and version).
// Returns (zero, nil, nil) if the subject is not an OCI manifest. The manifest
// references [ociImageSpecV1.DescriptorEmptyJSON] as config and layer. The
// caller must push that blob before the manifest.
func OwnershipReferrer(ctx context.Context, subject ociImageSpecV1.Descriptor, artifact descriptor.Artifact, component string, version string) (ociImageSpecV1.Descriptor, []byte, error) {
	if !introspection.IsOCICompliantManifest(subject) {
		slog.DebugContext(ctx, "skipping ownership referrer: subject is not an OCI manifest", "mediaType", subject.MediaType, "digest", subject.Digest.String())
		return ociImageSpecV1.Descriptor{}, nil, nil
	}

	kind, err := artifactKind(artifact)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, err
	}
	meta := artifact.GetElementMeta()
	artifactValue, err := marshalArtifactAnnotation(meta.ToIdentity(), kind)
	if err != nil {
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to build ownership artifact annotation: %w", err)
	}

	emptyDesc := ociImageSpecV1.DescriptorEmptyJSON
	manifest := ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: annotations.OwnershipArtifactType,
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
		return ociImageSpecV1.Descriptor{}, nil, fmt.Errorf("failed to marshal ownership referrer manifest: %w", err)
	}

	desc := ociImageSpecV1.Descriptor{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: annotations.OwnershipArtifactType,
		Digest:       digest.FromBytes(body),
		Size:         int64(len(body)),
	}
	return desc, body, nil
}

// artifactKind reports the [annotations.ArtifactKind] for the given artifact.
func artifactKind(artifact descriptor.Artifact) (annotations.ArtifactKind, error) {
	if _, ok := artifact.(*descriptor.Resource); !ok {
		return "", fmt.Errorf("unsupported artifact type: %T", artifact)
	}
	return annotations.ArtifactKindResource, nil
}

// marshalArtifactAnnotation serialises the {identity, kind} value stored under
// [annotations.ArtifactAnnotationKey] on an ownership referrer.
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
