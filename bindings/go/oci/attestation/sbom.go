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

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/repository"
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

// DiscoverSBOMs resolves reference in store and returns every SBOM attestation attached
// to the images matching the requested platform, or to every platform in the index when
// WithAllSBOMPlatforms is given.
//
// Returns the following set of errors:
// - ErrNotAnIndex if the reference is a plain manifest
// - ErrPlatformNotFound if there was no platform for the given value
// - ErrNoAttestation if there was no SBOM.
func DiscoverSBOMs(ctx context.Context, store spec.Store, reference string, opts ...repository.SBOMOption) ([]repository.SBOM, error) {
	o := repository.NewSBOMOptions(opts...)

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

	selected := selectPlatforms(images, o.Platform, o.AllPlatforms)
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrPlatformNotFound, reference)
	}

	var sboms []repository.SBOM
	var attested int
	for _, image := range selected {
		var manifests []ociImageSpecV1.Descriptor
		for _, attestation := range attestations {
			if attestation.Annotations[AnnotationDockerReferenceDigest] == image.Digest.String() {
				manifests = append(manifests, attestation)
			}
		}
		if len(manifests) == 0 {
			slog.DebugContext(ctx, "skipping image without an attestation",
				slog.String("image", image.Digest.String()),
				slog.Any("platform", image.Platform))
			continue
		}
		attested++

		for _, manifest := range manifests {
			found, err := sbomsFromAttestation(ctx, store, o, image, manifest)
			if err != nil {
				return nil, err
			}
			sboms = append(sboms, found...)
		}
	}

	if attested == 0 {
		return nil, fmt.Errorf("%w: %q", ErrNoAttestation, reference)
	}

	if len(sboms) == 0 {
		return nil, fmt.Errorf("%w: %q has no attestation of predicate types %v", ErrNoAttestation, reference, o.PredicateTypes)
	}

	return sboms, nil
}

// sbomsFromAttestation reads one attestation manifest and returns the SBOM statements
// among its layers.
func sbomsFromAttestation(
	ctx context.Context,
	store spec.Store,
	o *repository.SBOMOptions,
	image, attestation ociImageSpecV1.Descriptor,
) ([]repository.SBOM, error) {
	var manifest ociImageSpecV1.Manifest
	if err := fetchJSON(ctx, store, attestation, &manifest); err != nil {
		return nil, fmt.Errorf("reading attestation manifest %s failed: %w", attestation.Digest, err)
	}

	var sboms []repository.SBOM
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
		if !slices.Contains(o.PredicateTypes, predicateType) {
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

		sboms = append(sboms, repository.SBOM{
			Platform:      platform,
			Digest:        image.Digest,
			PredicateType: predicateType,
			Name:          named.Name,
			Data:          envelope.Predicate,
		})
	}

	return sboms, nil
}

// selectPlatforms picks the image entries to inspect. When all is set every entry
// that has a platform is returned and "want" is ignored. Otherwise, the first entry
// matching "want" is returned on its own.
func selectPlatforms(images []ociImageSpecV1.Descriptor, want ociImageSpecV1.Platform, all bool) []ociImageSpecV1.Descriptor {
	var selected []ociImageSpecV1.Descriptor
	for _, image := range images {
		if image.Platform == nil {
			continue
		}
		if !all && !platformMatches(*image.Platform, want) {
			continue
		}
		selected = append(selected, image)
		if !all {
			break
		}
	}
	return selected
}

// platformMatches returns if "want" is satisfied.
func platformMatches(have ociImageSpecV1.Platform, want ociImageSpecV1.Platform) bool {
	switch {
	case want.Architecture != "" && want.Architecture != have.Architecture:
		return false
	case want.OS != "" && want.OS != have.OS:
		return false
	case want.Variant != "" && want.Variant != have.Variant:
		return false
	case want.OSVersion != "" && want.OSVersion != have.OSVersion:
		return false
	case len(want.OSFeatures) > 0 && !osFeaturesMatch(have.OSFeatures, want.OSFeatures):
		return false
	default:
		return true
	}
}

// osFeaturesMatch compares two OS feature sets independently of the order they are
// listed in, since the OCI image specification does not define one.
func osFeaturesMatch(have, want []string) bool {
	if len(have) != len(want) {
		return false
	}
	haveSorted, wantSorted := slices.Clone(have), slices.Clone(want)
	slices.Sort(haveSorted)
	slices.Sort(wantSorted)
	return slices.Equal(haveSorted, wantSorted)
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
