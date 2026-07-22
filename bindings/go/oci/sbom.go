package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	intoto "github.com/in-toto/attestation/go/v1"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry"
	"google.golang.org/protobuf/encoding/protojson"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SBOM-related media types and annotation keys used to discover Software Bill of
// Materials (SBOM) documents attached to an OCI image. None of these are defined
// by the OCM spec; they follow the conventions established by BuildKit/buildx
// (in-index attestations) and the OCI image-spec Referrers API.
const (
	// MediaTypeInToto is the media type of an in-toto statement layer, as used by
	// buildx attestation manifests to carry SBOM and provenance predicates.
	MediaTypeInToto = "application/vnd.in-toto+json"

	// MediaTypeSPDXJSON is the media type of a raw SPDX JSON document, as used by
	// some registries when attaching an SBOM via the Referrers API.
	MediaTypeSPDXJSON = "application/spdx+json"

	// MediaTypeCycloneDXJSON is the media type of a raw CycloneDX JSON document.
	MediaTypeCycloneDXJSON = "application/vnd.cyclonedx+json"

	// AnnotationInTotoPredicateType is the annotation key on an in-toto layer that
	// records which predicate the statement carries.
	AnnotationInTotoPredicateType = "in-toto.io/predicate-type"

	// PredicateTypeSPDX is the in-toto predicate type for an SPDX SBOM document.
	PredicateTypeSPDX = "https://spdx.dev/Document"

	// PredicateTypeCycloneDX is the in-toto predicate type for a CycloneDX SBOM.
	PredicateTypeCycloneDX = "https://cyclonedx.org/bom"

	// AnnotationDockerReferenceType is the annotation key BuildKit places on the
	// attestation manifest entry of an image index.
	AnnotationDockerReferenceType = "vnd.docker.reference.type"

	// DockerReferenceTypeAttestationManifest is the value of
	// AnnotationDockerReferenceType marking an in-index attestation manifest.
	DockerReferenceTypeAttestationManifest = "attestation-manifest"

	// AnnotationDockerReferenceDigest is the annotation key on an attestation
	// manifest entry pointing at the image manifest it describes.
	AnnotationDockerReferenceDigest = "vnd.docker.reference.digest"
)

// sbomReferrerArtifactTypes are the artifact types queried against the Referrers
// API when looking for an SBOM attached as an OCI referrer (mechanism B).
var sbomReferrerArtifactTypes = []string{
	MediaTypeSPDXJSON,
	MediaTypeCycloneDXJSON,
}

// SBOM is a Software Bill of Materials discovered for an OCI image, together with
// the metadata needed to name and interpret it.
type SBOM struct {
	// Blob is the SBOM document itself (e.g. an SPDX or CycloneDX JSON document).
	// For SBOMs found as buildx in-toto attestations, the surrounding in-toto
	// statement envelope has already been unwrapped and Blob is the bare
	// predicate. For SBOMs found via the Referrers API, Blob is the referrer
	// artifact's content as-is.
	Blob blob.ReadOnlyBlob
	// MediaType is the media type of Blob (e.g. application/spdx+json).
	MediaType string
	// PredicateType is the in-toto predicate type when the SBOM was found as a
	// buildx attestation layer; empty for SBOMs found via the Referrers API.
	PredicateType string
	// Format is a short, human-readable label of the SBOM format ("spdx" or
	// "cyclonedx") when it can be determined, otherwise empty.
	Format string
	// Subjects lists the in-toto statement subjects (name + digests) the SBOM
	// attests to, when discovered via an attestation. Nil for Referrers-API SBOMs.
	Subjects []*intoto.ResourceDescriptor
	// Platform is the image platform this SBOM describes, when discovered via a
	// buildx attestation manifest (resolved from the attestation's
	// vnd.docker.reference.digest back to the index's image manifest). Nil for
	// single-platform images and Referrers-API SBOMs.
	Platform *ociImageSpecV1.Platform
}

// ImageSBOMDownloader is the optional capability of a resource plugin that can
// fetch the SBOM(s) attached to an OCI image resource (as a buildx attestation
// or via the OCI Referrers API). The builtin OCI resource plugin implements it.
// It is defined here so that both the CLI (which discovers plugins at runtime)
// and input methods (which reach the OCI plugin at construction time) can share
// a single downloader contract without duplicating it.
type ImageSBOMDownloader interface {
	DownloadImageSBOMs(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) ([]SBOM, error)
}

// FetchImageSBOMs discovers and fetches the SBOM(s) attached to the given OCI
// image reference. It looks in two places, in order:
//
//  1. buildx in-index attestations: if the reference resolves to an image index
//     containing a manifest annotated vnd.docker.reference.type=attestation-manifest,
//     the in-toto layers of that manifest whose predicate type is an SBOM
//     (SPDX/CycloneDX) are returned.
//  2. the OCI Referrers API: artifacts with an SBOM artifactType whose subject is
//     the image manifest are returned.
//
// If no SBOM is found, an empty slice and a nil error are returned so callers can
// distinguish "no SBOM attached" from a fetch failure.
func (repo *Repository) FetchImageSBOMs(ctx context.Context, imageReference string) ([]SBOM, error) {
	store, err := repo.resolver.StoreForReference(ctx, imageReference)
	if err != nil {
		return nil, fmt.Errorf("resolving store for %q failed: %w", imageReference, err)
	}

	resolved, err := looseref.ParseReference(imageReference)
	if err != nil {
		return nil, fmt.Errorf("parsing image reference %q failed: %w", imageReference, err)
	}
	reference := resolved.ReferenceOrTag()

	rootDesc, err := store.Resolve(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("resolving reference %q failed: %w", imageReference, err)
	}

	// Mechanism A: buildx in-index attestation manifests.
	sboms, err := fetchAttestationSBOMs(ctx, store, rootDesc)
	if err != nil {
		return nil, err
	}
	if len(sboms) > 0 {
		return sboms, nil
	}

	// Mechanism B: OCI Referrers API.
	return fetchReferrerSBOMs(ctx, store, rootDesc)
}

// fetchAttestationSBOMs implements mechanism A. It only applies when rootDesc is
// an image index; a plain image manifest carries no buildx attestation entries.
func fetchAttestationSBOMs(ctx context.Context, store spec.Store, rootDesc ociImageSpecV1.Descriptor) ([]SBOM, error) {
	if rootDesc.MediaType != ociImageSpecV1.MediaTypeImageIndex {
		return nil, nil
	}

	index, err := fetchIndex(ctx, store, rootDesc)
	if err != nil {
		return nil, err
	}

	// Map image-manifest digest -> platform, so an attestation manifest (which
	// points at the image it describes via vnd.docker.reference.digest) can be
	// labelled with that image's platform.
	platformByDigest := make(map[string]*ociImageSpecV1.Platform)
	for i := range index.Manifests {
		m := index.Manifests[i]
		if m.Annotations[AnnotationDockerReferenceType] == DockerReferenceTypeAttestationManifest {
			continue
		}
		if m.Platform != nil {
			platformByDigest[m.Digest.String()] = m.Platform
		}
	}

	var sboms []SBOM
	for _, m := range index.Manifests {
		if m.Annotations[AnnotationDockerReferenceType] != DockerReferenceTypeAttestationManifest {
			continue
		}
		platform := platformByDigest[m.Annotations[AnnotationDockerReferenceDigest]]
		manifest, err := fetchManifest(ctx, store, m)
		if err != nil {
			return nil, fmt.Errorf("fetching attestation manifest %q failed: %w", m.Digest, err)
		}
		for _, layer := range manifest.Layers {
			predicate := layer.Annotations[AnnotationInTotoPredicateType]
			format := sbomFormatForPredicate(predicate)
			if format == "" {
				continue
			}
			sbom, err := sbomFromAttestationLayer(ctx, store, layer, predicate, format)
			if err != nil {
				return nil, err
			}
			sbom.Platform = platform
			sboms = append(sboms, *sbom)
		}
	}
	return sboms, nil
}

// sbomFromAttestationLayer fetches an in-toto attestation layer, parses and
// validates the statement, and returns the unwrapped SBOM predicate as its own
// blob with the format's canonical media type.
func sbomFromAttestationLayer(ctx context.Context, store spec.Store, layer ociImageSpecV1.Descriptor, predicateType, format string) (_ *SBOM, err error) {
	r, err := store.Fetch(ctx, layer)
	if err != nil {
		return nil, fmt.Errorf("fetching SBOM layer %q failed: %w", layer.Digest, err)
	}
	defer func() {
		err = errors.Join(err, r.Close())
	}()

	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading SBOM layer %q failed: %w", layer.Digest, err)
	}

	stmt := &intoto.Statement{}
	if err := protojson.Unmarshal(raw, stmt); err != nil {
		return nil, fmt.Errorf("parsing in-toto statement in layer %q failed: %w", layer.Digest, err)
	}
	if err := stmt.Validate(); err != nil {
		return nil, fmt.Errorf("validating in-toto statement in layer %q failed: %w", layer.Digest, err)
	}
	if stmt.Predicate == nil {
		return nil, fmt.Errorf("in-toto statement in layer %q has no predicate", layer.Digest)
	}

	// Re-marshal the predicate (a structpb.Struct) as the bare SBOM document.
	predicateJSON, err := protojson.Marshal(stmt.Predicate)
	if err != nil {
		return nil, fmt.Errorf("serializing SBOM predicate from layer %q failed: %w", layer.Digest, err)
	}

	mediaType := sbomMediaTypeForFormat(format)
	return &SBOM{
		Blob:          inmemory.New(bytes.NewReader(predicateJSON), inmemory.WithMediaType(mediaType)),
		MediaType:     mediaType,
		PredicateType: predicateType,
		Format:        format,
		Subjects:      stmt.Subject,
	}, nil
}

// fetchReferrerSBOMs implements mechanism B via the Referrers API.
func fetchReferrerSBOMs(ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor) ([]SBOM, error) {
	if _, ok := store.(registry.ReferrerLister); !ok {
		// Referrers are not supported by this store (e.g. some archive stores).
		return nil, nil
	}

	var sboms []SBOM
	for _, artifactType := range sbomReferrerArtifactTypes {
		refs, err := registry.Referrers(ctx, store, subject, artifactType)
		if err != nil {
			return nil, fmt.Errorf("listing referrers of type %q failed: %w", artifactType, err)
		}
		for _, ref := range refs {
			b, err := fetchLayerBlob(ctx, store, ref)
			if err != nil {
				return nil, fmt.Errorf("fetching SBOM referrer %q failed: %w", ref.Digest, err)
			}
			sboms = append(sboms, SBOM{
				Blob:      b,
				MediaType: ref.MediaType,
				Format:    sbomFormatForMediaType(artifactType),
			})
		}
	}
	return sboms, nil
}

func fetchIndex(ctx context.Context, store spec.Store, desc ociImageSpecV1.Descriptor) (*ociImageSpecV1.Index, error) {
	var index ociImageSpecV1.Index
	if err := fetchJSON(ctx, store, desc, &index); err != nil {
		return nil, fmt.Errorf("decoding image index %q failed: %w", desc.Digest, err)
	}
	return &index, nil
}

func fetchManifest(ctx context.Context, store spec.Store, desc ociImageSpecV1.Descriptor) (*ociImageSpecV1.Manifest, error) {
	var manifest ociImageSpecV1.Manifest
	if err := fetchJSON(ctx, store, desc, &manifest); err != nil {
		return nil, fmt.Errorf("decoding image manifest %q failed: %w", desc.Digest, err)
	}
	return &manifest, nil
}

func fetchJSON(ctx context.Context, store spec.Store, desc ociImageSpecV1.Descriptor, dest any) (err error) {
	r, err := store.Fetch(ctx, desc)
	if err != nil {
		return fmt.Errorf("fetching %q failed: %w", desc.Digest, err)
	}
	defer func() {
		err = errors.Join(err, r.Close())
	}()
	return json.NewDecoder(r).Decode(dest)
}

// fetchLayerBlob fetches a single layer/artifact blob into a blob.ReadOnlyBlob,
// mirroring the pattern used by getLocalBlobFromIndexOrManifest. The returned
// blob owns the underlying reader and must be consumed/closed by the caller.
func fetchLayerBlob(ctx context.Context, store spec.Store, desc ociImageSpecV1.Descriptor) (blob.ReadOnlyBlob, error) {
	data, err := store.Fetch(ctx, desc)
	if err != nil {
		return nil, err
	}
	// data is not closed here: ownership passes to the blob.
	b := ociblob.NewDescriptorBlob(data, desc)
	if actual, known := b.Digest(); known && actual != desc.Digest.String() {
		return nil, fmt.Errorf("digest mismatch: expected %q, got %q", desc.Digest, actual)
	}
	return b, nil
}

func sbomFormatForPredicate(predicateType string) string {
	switch predicateType {
	case PredicateTypeSPDX:
		return "spdx"
	case PredicateTypeCycloneDX:
		return "cyclonedx"
	default:
		return ""
	}
}

func sbomFormatForMediaType(mediaType string) string {
	switch mediaType {
	case MediaTypeSPDXJSON:
		return "spdx"
	case MediaTypeCycloneDXJSON:
		return "cyclonedx"
	default:
		return ""
	}
}

// sbomMediaTypeForFormat returns the canonical JSON media type for an unwrapped
// SBOM document of the given format.
func sbomMediaTypeForFormat(format string) string {
	switch format {
	case "spdx":
		return MediaTypeSPDXJSON
	case "cyclonedx":
		return MediaTypeCycloneDXJSON
	default:
		return "application/json"
	}
}
