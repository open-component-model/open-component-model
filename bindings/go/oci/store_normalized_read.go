package oci

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	normalizedlayout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
)

// errNotAccessReferrer marks a referrer of the normalized manifest that is not an access referrer,
// such as a cosign signature.
var errNotAccessReferrer = errors.New("not an access referrer")

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
// An access referrer is valid only if its subject is the normalized manifest, its descriptor
// re-normalizes to the normalized layer (bind check), and that descriptor satisfies the digest
// rules of normalizedlayout.ValidateDigests. Normalisation drops signatures, accesses and
// non-signing labels, so several valid referrers may carry different descriptors (e.g. after an
// OCM signature was added by re-adding the version). Selection therefore never picks between them:
// the valid referrers found via the referrers API must all carry the same descriptor bytes;
// otherwise the read fails as ambiguous.
func getNormalizedComponentVersion(ctx context.Context, store spec.Store, normalized ociImageSpecV1.Descriptor, normManifest *ociImageSpecV1.Manifest, unmarshal ocidescriptor.UnmarshalFunc) (*storedComponentVersion, error) {
	normalized.ArtifactType = ocidescriptor.ArtifactTypeNormalizedDescriptor
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

	predecessors, err := store.Predecessors(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to discover access referrers of normalized manifest %s: %w", normalized.Digest, err)
	}
	var valid []*accessReferrer
	var rejected []error
	for _, p := range predecessors {
		ref, err := loadAccessReferrer(ctx, store, p, normalized, signedNormalized, unmarshal)
		if errors.Is(err, errNotAccessReferrer) {
			continue
		}
		if err != nil {
			rejected = append(rejected, fmt.Errorf("access referrer %s: %w", p.Digest, err))
			continue
		}
		valid = append(valid, ref)
	}
	if len(valid) == 0 {
		if len(rejected) == 0 {
			return nil, fmt.Errorf("no access referrer found for normalized manifest %s", normalized.Digest)
		}
		return nil, fmt.Errorf("no valid access referrer found for normalized manifest %s: %w", normalized.Digest, errors.Join(rejected...))
	}
	// Re-adding a version (e.g. to persist a signature) and third-party pushes leave several valid
	// access referrers of the same M; the most recently created one reflects the last writer. Ties
	// (equal or absent timestamps) break by the lowest manifest digest for a reproducible choice.
	slices.SortFunc(valid, func(a, b *accessReferrer) int {
		if c := b.created.Compare(a.created); c != 0 {
			return c
		}
		return strings.Compare(a.digest, b.digest)
	})
	return valid[0].stored(), nil
}

// accessReferrer is a validated access referrer of a normalized manifest.
type accessReferrer struct {
	digest          string
	created         time.Time
	desc            *descriptor.Descriptor
	descriptorBytes []byte
	manifest        *ociImageSpecV1.Manifest
	index           *ociImageSpecV1.Index
}

func (a *accessReferrer) stored() *storedComponentVersion {
	return &storedComponentVersion{descriptor: a.desc, manifest: a.manifest, index: a.index, layout: LayoutNormalized}
}

// loadAccessReferrer fetches target and validates it as access referrer of normalized. It returns
// an error wrapping errNotAccessReferrer if target is some other referrer.
func loadAccessReferrer(ctx context.Context, store spec.Store, target, normalized ociImageSpecV1.Descriptor, signedNormalized []byte, unmarshal ocidescriptor.UnmarshalFunc) (*accessReferrer, error) {
	raw, err := content.FetchAll(ctx, store, target)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}
	var header struct {
		MediaType    string                     `json:"mediaType"`
		ArtifactType string                     `json:"artifactType"`
		Subject      *ociImageSpecV1.Descriptor `json:"subject"`
		Annotations  map[string]string          `json:"annotations"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, fmt.Errorf("failed to decode: %w", err)
	}
	if header.ArtifactType != ocidescriptor.ArtifactTypeAccessDescriptor {
		return nil, fmt.Errorf("artifact type %q: %w", header.ArtifactType, errNotAccessReferrer)
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
	return &accessReferrer{
		digest:          target.Digest.String(),
		created:         parseCreated(header.Annotations[ociImageSpecV1.AnnotationCreated]),
		desc:            full,
		descriptorBytes: layerBytes,
		manifest:        &manifest,
		index:           index,
	}, nil
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

// peekArtifactType fetches a manifest/index and returns its artifactType from the body. This is
// needed because registries do not populate Descriptor.ArtifactType when resolving a tag; the value
// lives inside the manifest JSON.
func peekArtifactType(ctx context.Context, store spec.Store, target ociImageSpecV1.Descriptor) (string, error) {
	raw, err := content.FetchAll(ctx, store, target)
	if err != nil {
		return "", err
	}
	var peek struct {
		ArtifactType string `json:"artifactType"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return "", err
	}
	return peek.ArtifactType, nil
}
