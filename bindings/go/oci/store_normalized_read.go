package oci

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/registry"
	slogcontext "github.com/veqryn/slog-context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/log"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	normalizedlayout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
)

// GetNormalizedComponentVersion reads the component descriptor stored in the normalized layout
// under the normalized manifest `normalized`. See getNormalizedComponentVersion for how the access
// referrer is selected and validated.
func GetNormalizedComponentVersion(ctx context.Context, store spec.Store, normalized ociImageSpecV1.Descriptor, unmarshal ocidescriptor.UnmarshalFunc) (*descriptor.Descriptor, error) {
	normManifestRaw, err := content.FetchAll(ctx, store, normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch normalized manifest: %w", err)
	}
	var normManifest ociImageSpecV1.Manifest
	if err := json.Unmarshal(normManifestRaw, &normManifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal normalized manifest: %w", err)
	}
	cv, err := getNormalizedComponentVersion(ctx, store, normalized, &normManifest, unmarshal)
	if err != nil {
		return nil, err
	}
	return cv.descriptor, nil
}

// getNormalizedComponentVersion selects the access referrer of an already fetched normalized
// manifest and returns its descriptor together with the manifest (and index) listing its local
// blobs.
//
// Every write adds an access referrer, so a version can have several: re-adding it (e.g. to persist
// an OCM signature) and third-party pushes leave the earlier ones in place. Normalisation drops
// signatures, accesses and non-signing labels, so they may carry different descriptors. The read
// returns the newest valid one.
//
// Candidates are the referrers listed with the access artifact type, ordered by the creation time
// the listing reports for them, see compareCreated. They are fetched and validated in that order
// until one is valid, see loadAccessReferrer. With a valid newest candidate the read costs the same
// however many signatures and superseded access referrers the version has. An invalid candidate is
// skipped with a warning instead of failing the read: otherwise a single referrer stamped with a
// time in the future would make the version unreadable, since later writes never sort before it
// and registries often cannot delete it.
func getNormalizedComponentVersion(ctx context.Context, store spec.Store, normalized ociImageSpecV1.Descriptor, normManifest *ociImageSpecV1.Manifest, unmarshal ocidescriptor.UnmarshalFunc) (*storedComponentVersion, error) {
	if len(normManifest.Layers) != 1 {
		return nil, fmt.Errorf("normalized manifest %s must have exactly one layer, got %d", normalized.Digest, len(normManifest.Layers))
	}
	normLayer := normManifest.Layers[0]
	if normLayer.MediaType != ocidescriptor.MediaTypeComponentDescriptorNormalizedJSON {
		return nil, fmt.Errorf("normalized manifest %s has layer media type %q, expected %q", normalized.Digest, normLayer.MediaType, ocidescriptor.MediaTypeComponentDescriptorNormalizedJSON)
	}
	signedNormalized, err := content.FetchAll(ctx, store, normLayer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch normalized descriptor layer: %w", err)
	}

	candidates, err := registry.Referrers(ctx, store, normalized, ocidescriptor.ArtifactTypeAccessDescriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to list access referrers of normalized manifest %s: %w", normalized.Digest, err)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no access referrer found for normalized manifest %s", normalized.Digest)
	}
	slices.SortFunc(candidates, compareCreated)

	var rejected []error
	for _, candidate := range candidates {
		cv, err := loadAccessReferrer(ctx, store, candidate, normalized, signedNormalized, unmarshal)
		if err == nil {
			return cv, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("failed to read access referrer %s of normalized manifest %s: %w", candidate.Digest, normalized.Digest, err)
		}
		slogcontext.Log(ctx, slog.LevelWarn, "skipping invalid access referrer of normalized manifest",
			log.DescriptorLogAttr(candidate),
			slog.String("normalized", normalized.Digest.String()),
			slog.Any("error", err))
		rejected = append(rejected, fmt.Errorf("access referrer %s: %w", candidate.Digest, err))
	}
	return nil, fmt.Errorf("no valid access referrer found for normalized manifest %s: %w", normalized.Digest, errors.Join(rejected...))
}

// compareCreated orders listed referrers by their org.opencontainers.image.created annotation,
// newest first. Ties (equal or absent timestamps) break by the lowest digest for a reproducible
// order.
func compareCreated(a, b ociImageSpecV1.Descriptor) int {
	createdA := parseCreated(a.Annotations[ociImageSpecV1.AnnotationCreated])
	createdB := parseCreated(b.Annotations[ociImageSpecV1.AnnotationCreated])
	if c := createdB.Compare(createdA); c != 0 {
		return c
	}
	return strings.Compare(a.Digest.String(), b.Digest.String())
}

// loadAccessReferrer fetches target and validates it as access referrer of normalized: its
// artifact type is the access one, its subject is the normalized manifest, its descriptor
// re-normalizes to the normalized layer signedNormalized (bind check), and that descriptor
// satisfies the digest rules of normalizedlayout.ValidateDigests.
func loadAccessReferrer(ctx context.Context, store spec.Store, target, normalized ociImageSpecV1.Descriptor, signedNormalized []byte, unmarshal ocidescriptor.UnmarshalFunc) (*storedComponentVersion, error) {
	raw, err := content.FetchAll(ctx, store, target)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}
	var header struct {
		MediaType    string                     `json:"mediaType"`
		ArtifactType string                     `json:"artifactType"`
		Subject      *ociImageSpecV1.Descriptor `json:"subject"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, fmt.Errorf("failed to decode: %w", err)
	}
	if header.ArtifactType != ocidescriptor.ArtifactTypeAccessDescriptor {
		return nil, fmt.Errorf("artifact type is %q, expected %q", header.ArtifactType, ocidescriptor.ArtifactTypeAccessDescriptor)
	}
	if header.Subject == nil || header.Subject.Digest != normalized.Digest {
		return nil, fmt.Errorf("subject is not the normalized manifest %s", normalized.Digest)
	}

	mediaType := target.MediaType
	if mediaType == "" {
		mediaType = header.MediaType
	}
	var manifest ociImageSpecV1.Manifest
	var index *ociImageSpecV1.Index
	switch mediaType {
	case ociImageSpecV1.MediaTypeImageManifest:
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, fmt.Errorf("failed to decode manifest: %w", err)
		}
	case ociImageSpecV1.MediaTypeImageIndex:
		index = &ociImageSpecV1.Index{}
		if err := json.Unmarshal(raw, index); err != nil {
			return nil, fmt.Errorf("failed to decode index: %w", err)
		}
		if len(index.Manifests) == 0 || index.Manifests[0].MediaType != ociImageSpecV1.MediaTypeImageManifest {
			return nil, fmt.Errorf("access index must list the access manifest first")
		}
		innerRaw, err := content.FetchAll(ctx, store, index.Manifests[0])
		if err != nil {
			return nil, fmt.Errorf("failed to fetch access manifest: %w", err)
		}
		if err := json.Unmarshal(innerRaw, &manifest); err != nil {
			return nil, fmt.Errorf("failed to decode access manifest: %w", err)
		}
		if manifest.ArtifactType != ocidescriptor.ArtifactTypeAccessDescriptor {
			return nil, fmt.Errorf("first manifest of access index has artifact type %q, expected %q", manifest.ArtifactType, ocidescriptor.ArtifactTypeAccessDescriptor)
		}
	default:
		return nil, fmt.Errorf("unsupported media type %q", mediaType)
	}

	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("access manifest has no layers")
	}
	layer := manifest.Layers[0]
	layerBytes, err := content.FetchAll(ctx, store, layer)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch access descriptor layer: %w", err)
	}
	full, err := ocidescriptor.SingleFileDecodeDescriptor(bytes.NewReader(layerBytes), layer.MediaType, unmarshal)
	if err != nil {
		return nil, fmt.Errorf("failed to decode access descriptor: %w", err)
	}
	if err := normalizedlayout.VerifyNormalizedMatchesAccess(signedNormalized, full); err != nil {
		return nil, fmt.Errorf("bind check failed: %w", err)
	}
	if err := normalizedlayout.ValidateDigests(full); err != nil {
		return nil, err
	}
	return &storedComponentVersion{descriptor: full, manifest: &manifest, index: index, layout: LayoutNormalized}, nil
}

// parseCreated parses an org.opencontainers.image.created annotation. An absent or malformed value
// yields the zero time, which sorts oldest, so a stamped referrer always wins over an unstamped one.
func parseCreated(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}
