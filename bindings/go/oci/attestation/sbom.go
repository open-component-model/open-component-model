// Package attestation discovers build attestations attached to an OCI artifact.
//
// At the moment, the implementation in here only supports BuildKit specific structure.
//
// BuilKit provides the following OCI layering structure:
//
//	index
//	├── manifests[0]  platform: linux/arm64                  the image
//	└── manifests[1]  platform: unknown/unknown              the attestation
//	      annotations:
//	        vnd.docker.reference.type:   attestation-manifest
//	        vnd.docker.reference.digest: <digest of manifests[0]>
//	      layers[*]  mediaType: application/vnd.in-toto+json
//	        annotations:
//	          in-toto.io/predicate-type: https://spdx.dev/Document
//
// Each in-toto layer wraps the actual document in a statement envelope. The SBOM is the
// predicate of that envelope.
package attestation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
)

var (
	// ErrNotAnIndex is returned when the reference does not resolve to an image index.
	ErrNotAnIndex = errors.New("reference does not resolve to an image index")
	// ErrNoAttestation is returned when the index has no SBOM attestation for the
	// requested platform.
	ErrNoAttestation = errors.New("no buildx SBOM attestation found")
	// ErrPlatformNotFound is returned when the index has no image for the requested
	// platform.
	ErrPlatformNotFound = errors.New("platform not present in index")
)

// SBOM is one SBOM document discovered inside a build attestation.
type SBOM struct {
	// Platform of the image this SBOM describes. Never the unknown/unknown placeholder
	// carried by the attestation manifest itself.
	Platform ociImageSpecV1.Platform
	// Subject is the image manifest the attestation points at.
	Subject ociImageSpecV1.Descriptor
	// Manifest is the attestation manifest holding the statement.
	Manifest ociImageSpecV1.Descriptor
	// Layer is the in-toto statement layer within Manifest.
	Layer ociImageSpecV1.Descriptor
	// PredicateType names the kind of document Predicate holds, for example
	// PredicateTypeSPDX.
	PredicateType string
	// Name is the document's own name. Make it configurable for BuildKit's naming
	// convention.
	Name string
	// Statement is the complete in-toto statement, byte for byte as stored.
	Statement []byte
	// Predicate is the SBOM document itself
	Predicate json.RawMessage
}

// IsCore returns true if the given SBOM is the core and not the stage.
// This is specific to BuildKit.
func (s SBOM) IsCore() bool {
	return s.Name == CoreSBOMName
}

// Core finds the Core SBOM for a given artifact.
func Core(sboms []SBOM) (SBOM, bool) {
	if len(sboms) == 1 {
		return sboms[0], true
	}
	for _, sbom := range sboms {
		if sbom.IsCore() {
			return sbom, true
		}
	}
	return SBOM{}, false
}

// MediaType returns the right media-type given the PredicateType.
func (s SBOM) MediaType() string {
	switch s.PredicateType {
	case PredicateTypeSPDX:
		return MediaTypeSPDXJSON
	case PredicateTypeCycloneDX:
		return MediaTypeCycloneDXJSON
	default:
		return "application/json"
	}
}

// DiscoverSBOMs resolves reference in store and returns every SBOM attestation attached
// to the image matching the requested platform.
//
// BuildKit creates _several_ entries for a single attestation so those are normal.
// Returns the following set of errors:
// - ErrNotAnIndex if the reference is a plain manifest
// - ErrPlatformNotFound if there was no platform for the given value
// - ErrNoAttestation if there was no SBOM.
func DiscoverSBOMs(ctx context.Context, store spec.Store, reference string, opts ...Option) ([]SBOM, error) {
	o := newOptions(opts...)

	parsed, err := looseref.ParseReference(reference)
	if err != nil {
		return nil, fmt.Errorf("parsing reference %q failed: %w", reference, err)
	}

	root, err := store.Resolve(ctx, parsed.ReferenceOrTag())
	if err != nil {
		return nil, fmt.Errorf("resolving reference %q failed: %w", reference, err)
	}

	if !isIndex(root.MediaType) {
		return nil, fmt.Errorf("%w: %q has media type %q", ErrNotAnIndex, reference, root.MediaType)
	}

	var index ociImageSpecV1.Index
	if err := fetchJSON(ctx, store, root, &index); err != nil {
		return nil, fmt.Errorf("reading image index of %q failed: %w", reference, err)
	}

	var images, attestations []ociImageSpecV1.Descriptor
	for _, entry := range index.Manifests {
		switch {
		case entry.Annotations[AnnotationDockerReferenceType] != ReferenceTypeAttestationManifest:
			images = append(images, entry)
		case entry.Annotations[AnnotationDockerReferenceDigest] != "":
			attestations = append(attestations, entry)
		}
	}

	image, ok := selectPlatform(images, o.platform)
	if !ok {
		return nil, fmt.Errorf("%w: %q has no image for %s, available: %s",
			ErrPlatformNotFound, reference, formatPlatform(o.platform), describeAvailablePlatforms(images, attestations))
	}

	var manifests []ociImageSpecV1.Descriptor
	for _, attestation := range attestations {
		if attestation.Annotations[AnnotationDockerReferenceDigest] == image.Digest.String() {
			manifests = append(manifests, attestation)
		}
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("%w: %q has no attestation for %s, available: %s",
			ErrNoAttestation, reference, formatPlatform(o.platform), describeAvailablePlatforms(images, attestations))
	}

	var sboms []SBOM
	for _, manifest := range manifests {
		found, err := sbomsFromAttestation(ctx, store, o, image, manifest)
		if err != nil {
			return nil, err
		}
		sboms = append(sboms, found...)
	}

	if len(sboms) == 0 {
		return nil, fmt.Errorf("%w: %q has attestations for %s but none of predicate type %s",
			ErrNoAttestation, reference, formatPlatform(o.platform), strings.Join(o.predicateTypes, ", "))
	}

	return sboms, nil
}

// sbomsFromAttestation reads one attestation manifest and returns the SBOM statements
// among its layers.
func sbomsFromAttestation(
	ctx context.Context,
	store spec.Store,
	o *options,
	image, attestation ociImageSpecV1.Descriptor,
) ([]SBOM, error) {
	var manifest ociImageSpecV1.Manifest
	if err := fetchJSON(ctx, store, attestation, &manifest); err != nil {
		return nil, fmt.Errorf("reading attestation manifest %s failed: %w", attestation.Digest, err)
	}

	var sboms []SBOM
	for _, layer := range manifest.Layers {
		if layer.MediaType != MediaTypeInTotoStatement {
			continue
		}

		predicateType := layer.Annotations[AnnotationInTotoPredicateType]
		if predicateType == "" {
			slog.DebugContext(ctx, "skipping in-toto layer without a predicate type",
				slog.String("layer", layer.Digest.String()),
				slog.String("attestation", attestation.Digest.String()))
			continue
		}
		if !slices.Contains(o.predicateTypes, predicateType) {
			slog.DebugContext(ctx, "skipping in-toto layer of unwanted predicate type",
				slog.String("layer", layer.Digest.String()),
				slog.String("predicateType", predicateType))
			continue
		}

		statement, err := content.FetchAll(ctx, store, layer)
		if err != nil {
			return nil, fmt.Errorf("reading in-toto statement %s failed: %w", layer.Digest, err)
		}

		var envelope struct {
			Predicate json.RawMessage `json:"predicate"`
		}
		if err := json.Unmarshal(statement, &envelope); err != nil {
			return nil, fmt.Errorf("malformed sbom in attestation %s: in-toto statement %s of predicate type %s is not valid json: %w",
				attestation.Digest, layer.Digest, predicateType, err)
		}
		if len(envelope.Predicate) == 0 || string(envelope.Predicate) == "null" {
			return nil, fmt.Errorf("malformed sbom in attestation %s: in-toto statement %s of predicate type %s carries no predicate document",
				attestation.Digest, layer.Digest, predicateType)
		}

		var named struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(envelope.Predicate, &named)

		platform := ociImageSpecV1.Platform{}
		if image.Platform != nil {
			platform = *image.Platform
		}

		sboms = append(sboms, SBOM{
			Platform:      platform,
			Subject:       image,
			Manifest:      attestation,
			Layer:         layer,
			PredicateType: predicateType,
			Name:          named.Name,
			Statement:     statement,
			Predicate:     envelope.Predicate,
		})
	}

	return sboms, nil
}

// selectPlatform picks the image entry matching want. Attributes want leaves empty are
// not constrained, so asking for an architecture alone matches any operating system.
//
// Only entries partition kept as images reach this, so the unknown/unknown placeholder
// BuildKit puts on attestations is already out of the way.
func selectPlatform(images []ociImageSpecV1.Descriptor, want ociImageSpecV1.Platform) (ociImageSpecV1.Descriptor, bool) {
	for _, image := range images {
		if image.Platform == nil {
			continue
		}
		if want.Architecture != "" && want.Architecture != image.Platform.Architecture {
			continue
		}
		if want.OS != "" && want.OS != image.Platform.OS {
			continue
		}
		if want.Variant != "" && want.Variant != image.Platform.Variant {
			continue
		}
		return image, true
	}
	return ociImageSpecV1.Descriptor{}, false
}

// describeAvailablePlatforms renders the platforms an index offers, noting which of
// them carry no attestation, so a failure tells the caller what could have been asked
// for instead.
func describeAvailablePlatforms(images, attestations []ociImageSpecV1.Descriptor) string {
	if len(images) == 0 {
		return "none"
	}

	// Only the error paths need to know which images were attested, so the lookup is
	// built here rather than carried around for the common case.
	attested := make(map[string]bool, len(attestations))
	for _, attestation := range attestations {
		attested[attestation.Annotations[AnnotationDockerReferenceDigest]] = true
	}

	described := make([]string, 0, len(images))
	for _, image := range images {
		if image.Platform == nil {
			continue
		}
		entry := formatPlatform(*image.Platform)
		if !attested[image.Digest.String()] {
			entry += " (no attestation)"
		}
		described = append(described, entry)
	}
	if len(described) == 0 {
		return "none"
	}

	slices.Sort(described)
	return strings.Join(described, ", ")
}

// formatPlatform takes into consideration the Platform section `Variant`. Which is just
// a fancy way for defining special CPUs.
func formatPlatform(platform ociImageSpecV1.Platform) string {
	parts := []string{platform.OS, platform.Architecture}
	if platform.Variant != "" {
		parts = append(parts, platform.Variant)
	}
	return strings.Join(parts, "/")
}

func isIndex(mediaType string) bool {
	return mediaType == ociImageSpecV1.MediaTypeImageIndex ||
		mediaType == introspection.MediaTypeDockerManifestList
}

// fetchJSON reads a descriptor's content in full and decodes it.
func fetchJSON(ctx context.Context, store spec.Store, desc ociImageSpecV1.Descriptor, into any) error {
	raw, err := content.FetchAll(ctx, store, desc)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}
