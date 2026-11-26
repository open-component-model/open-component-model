// Package referrer lists the attestations attached to an OCI artifact through the OCI
// referrers API (cosign, vexctl, oras, syft, notary, ...).
//
// Discovery is metadata-only and unopinionated: it reads the referrers index and each
// small referrer manifest, never the document blob, and records the raw type signals
// (artifactType, in-toto predicate type from annotations, document media type) without
// classifying or filtering them. Every referrer is returned, signatures included.
package referrer

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content"
	"github.com/oras-project/oras-go/v3/registry"

	"ocm.software/open-component-model/bindings/go/oci/internal/attestation"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/repository"
)

const (
	// annotationSigstorePredicateType carries the in-toto predicate type on a cosign
	// Sigstore bundle referrer manifest, without fetching the bundle blob.
	annotationSigstorePredicateType = "dev.sigstore.bundle.predicateType"
	// annotationPredicateType is the plain in-toto predicate type annotation that oras
	// attach, cosign and BuildKit write on the referrer manifest, and that oras discover
	// reads. Distinct from the in-toto.io/predicate-type layer key.
	annotationPredicateType = "predicateType"
	// mediaTypeEmpty is the OCI empty descriptor oras uses as a config placeholder; it
	// is never the document layer.
	mediaTypeEmpty = "application/vnd.oci.empty.v1+json"
)

// DiscoverAttestations lists every referrer of subject in store and returns one
// Attestation per referrer, read from metadata only. Referrers whose manifest cannot be
// read are skipped with a warning so one bad entry never blocks the rest.
func DiscoverAttestations(ctx context.Context, store spec.Store, subject ociImageSpecV1.Descriptor, opts ...repository.AttestationOption) ([]repository.Attestation, error) {
	refs, err := registry.Referrers(ctx, store, subject, "")
	if err != nil {
		return nil, fmt.Errorf("listing referrers of %s failed: %w", subject.Digest, err)
	}

	out := make([]repository.Attestation, 0, len(refs))
	for _, ref := range refs {
		att, err := fromReferrer(ctx, store, subject, ref, opts...)
		if err != nil {
			slog.WarnContext(ctx, "skipping unreadable referrer",
				slog.String("referrer", ref.Digest.String()),
				slog.String("artifactType", ref.ArtifactType),
				slog.Any("error", err))
			continue
		}
		out = append(out, att)
	}
	return out, nil
}

// fromReferrer reads one referrer's metadata into an Attestation. It fetches the small
// referrer manifest (for its annotations and layer media type) but never the document.
func fromReferrer(ctx context.Context, store spec.Store, subject, ref ociImageSpecV1.Descriptor, opts ...repository.AttestationOption) (repository.Attestation, error) {
	var manifest ociImageSpecV1.Manifest
	if err := attestation.FetchJSON(ctx, store, ref, &manifest); err != nil {
		return repository.Attestation{}, fmt.Errorf("reading referrer manifest failed: %w", err)
	}

	att := repository.Attestation{
		Source:        repository.AttestationSourceOCIReferrer,
		ArtifactType:  cmp.Or(ref.ArtifactType, manifest.ArtifactType),
		PredicateType: predicateTypeFromMetadata(ref, manifest),
		MediaType:     documentMediaType(manifest),
		Digest:        subject.Digest.String(),
		ID:            ref.Digest.String(),
		CreatedAt:     cmp.Or(manifest.Annotations[ociImageSpecV1.AnnotationCreated], ref.Annotations[ociImageSpecV1.AnnotationCreated]),
		Annotations:   manifest.Annotations,
	}
	// The document layer digest is the content digest of the document bytes, read from the
	// manifest alone. It reveals duplicates: byte-identical documents attached in different
	// places share it, even though their referrer manifest digests differ.
	layer, ok := documentLayer(manifest)
	if ok {
		att.ContentDigest = layer.Digest.String()
	}

	if repository.NewAttestationOptions(opts...).IncludeData {
		if !ok {
			slog.DebugContext(ctx, "referrer carries no document layer, leaving data empty",
				slog.String("referrer", ref.Digest.String()))
		} else {
			data, err := content.FetchAll(ctx, store, layer)
			if err != nil {
				return repository.Attestation{}, fmt.Errorf("fetching referrer document layer %s failed: %w", layer.Digest, err)
			}
			att.Data = data
		}
	}
	return att, nil
}

// predicateTypeFromMetadata extracts the in-toto predicate type from annotations only: the
// Sigstore bundle annotation, the plain predicateType annotation (manifest or ref), or the
// in-toto.io/predicate-type annotation on the manifest or a layer. Empty when none is set.
func predicateTypeFromMetadata(ref ociImageSpecV1.Descriptor, manifest ociImageSpecV1.Manifest) string {
	for _, annotations := range []map[string]string{ref.Annotations, manifest.Annotations} {
		if pt := cmp.Or(
			annotations[annotationSigstorePredicateType],
			annotations[annotationPredicateType],
			annotations[attestation.AnnotationInTotoPredicateType],
		); pt != "" {
			return pt
		}
	}
	for _, layer := range manifest.Layers {
		if pt := cmp.Or(
			layer.Annotations[annotationPredicateType],
			layer.Annotations[attestation.AnnotationInTotoPredicateType],
		); pt != "" {
			return pt
		}
	}
	return ""
}

// documentLayer returns the first document layer descriptor, skipping the OCI empty
// config placeholder. The bool is false when a referrer carries no document layer.
func documentLayer(manifest ociImageSpecV1.Manifest) (ociImageSpecV1.Descriptor, bool) {
	for _, layer := range manifest.Layers {
		if layer.MediaType != mediaTypeEmpty {
			return layer, true
		}
	}
	return ociImageSpecV1.Descriptor{}, false
}

// documentMediaType returns the media type of the first document layer, skipping the OCI
// empty config placeholder.
func documentMediaType(manifest ociImageSpecV1.Manifest) string {
	if layer, ok := documentLayer(manifest); ok {
		return layer.MediaType
	}
	return ""
}
