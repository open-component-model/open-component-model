package repository

import (
	"cmp"
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// AttestationSource records where an attestation was discovered. It is the only
// provenance signal discovery can establish: oci-index is provably build-embedded,
// ocm-local travels with the component descriptor, oci-referrer is attached to the
// artifact in the registry.
type AttestationSource string

const (
	// AttestationSourceOCMLocal is a resource stored in the component descriptor,
	// linked to its subject via the "ocm.software/artifact-references" label.
	AttestationSourceOCMLocal AttestationSource = "ocm-local"
	// AttestationSourceOCIIndex is a BuildKit attestation manifest embedded in the
	// OCI image index of the artifact.
	AttestationSourceOCIIndex AttestationSource = "oci-index"
	// AttestationSourceOCIReferrer is a manifest attached to the artifact through the
	// OCI referrers API (cosign, vexctl, oras, syft, ...).
	AttestationSourceOCIReferrer AttestationSource = "oci-referrer"
)

// Attestation is one document attached to or describing an artifact: an SBOM, VEX,
// signature, SLSA provenance, vulnerability report, SARIF file or anything else.
//
// Discovery is deliberately unopinionated and metadata-only. It records the raw type
// signals verbatim (the OCI artifact type, the in-toto predicate type, the document
// media type) and never classifies them into a fixed vocabulary, so every kind of
// attestation is listed, not only those a hard-coded table recognises. Friendly grouping
// (sbom, vex, ...) and byte extraction are later, additive concerns; Data stays nil in a
// listing.
type Attestation struct {
	// Source is where the attestation was discovered.
	Source AttestationSource
	// ArtifactType is the raw OCI artifactType of a referrer, or the OCM resource type
	// for a local attestation. Empty when the source carries none.
	ArtifactType string
	// PredicateType is the raw in-toto predicate type, when the attestation is an
	// in-toto statement (BuildKit layer, cosign attest, Sigstore bundle). Empty otherwise.
	PredicateType string
	// MediaType is the raw media type of the document (or its in-toto envelope). Empty
	// when not determinable from metadata alone.
	MediaType string
	// Platform of the artifact this attestation describes. Zero when the source carries none.
	Platform Platform
	// Digest of the artifact the attestation describes (the subject). Shared by every
	// attestation of the same artifact, so it does not identify the document; use ID.
	Digest string
	// ID uniquely identifies the attestation within the subject: the referrer manifest
	// digest, the in-toto layer digest, or the local resource identity.
	ID string
	// ContentDigest is the digest of the document bytes themselves (the referrer or
	// in-toto document layer digest, or the ocm-local blob reference), as opposed to ID
	// which identifies the wrapper or location. Two attestations carrying byte-identical
	// documents share a ContentDigest even when attached in different places, so it is the
	// signal that reveals duplicates. Read from metadata only (no blob fetch); empty when
	// the source exposes no content digest.
	ContentDigest string
	// CreatedAt is the creation timestamp from the "org.opencontainers.image.created"
	// annotation of the referrer / attestation manifest, verbatim (RFC 3339). Empty when
	// the producer set none. It is the outer-container timestamp, readable from metadata
	// without fetching or verifying the document.
	CreatedAt string
	// Annotations is the raw annotation map of the referrer / attestation manifest, kept
	// whole so nothing is hidden. Nil when the source carries none.
	Annotations map[string]string
	// Data is the document itself. Nil in a metadata-only listing; a download fills it.
	Data []byte
}

// Type returns the most specific raw type signal present, preferring the in-toto
// predicate type, then the artifact type, then the media type. It is the single
// human-facing "what is this" string, shown verbatim without interpretation.
func (a Attestation) Type() string {
	return cmp.Or(a.PredicateType, a.ArtifactType, a.MediaType)
}

// String names the attestation without its contents, so formatting it cannot spill a
// whole document into a log line.
func (a Attestation) String() string {
	name := cmp.Or(a.Type(), "attestation")
	if platform := a.Platform.String(); platform != "" {
		return name + " (" + platform + ")"
	}
	return name
}

// AttestationOptions controls how an attestation is discovered. The zero value is the
// default: metadata-only, leaving Attestation.Data nil.
type AttestationOptions struct {
	// IncludeData asks the implementation to fill Attestation.Data with the primary
	// document layer's bytes, byte-for-byte. Default false keeps discovery metadata-only.
	IncludeData bool
}

// AttestationOption configures AttestationOptions.
type AttestationOption func(*AttestationOptions)

// WithAttestationData makes discovery fetch each attestation's primary document layer and
// set Attestation.Data to its bytes verbatim. Without it, Data stays nil.
func WithAttestationData() AttestationOption {
	return func(o *AttestationOptions) { o.IncludeData = true }
}

// NewAttestationOptions folds opts into a single AttestationOptions.
func NewAttestationOptions(opts ...AttestationOption) *AttestationOptions {
	o := &AttestationOptions{}
	for _, f := range opts {
		f(o)
	}
	return o
}

// AttestationDiscoverer retrieves the attestations attached to a resource's artifact
// (OCI referrers plus any BuildKit in-toto attestations in its image index),
// metadata-only. Credentials authenticate against the backing registry.
type AttestationDiscoverer interface {
	DiscoverAttestations(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed, opts ...AttestationOption) ([]Attestation, error)
}

// LocalAttestationDiscoverer retrieves the attestations of a resource whose artifact was
// copied into the component as a local blob (a by-value transfer, for example into a CTF).
type LocalAttestationDiscoverer interface {
	DiscoverLocalAttestations(ctx context.Context, component, version string, identity runtime.Identity, opts ...AttestationOption) ([]Attestation, error)
}

// ComponentAttestationDiscoverer retrieves the referrers attached to the component
// version artifact itself (describing the whole component rather than a single resource).
type ComponentAttestationDiscoverer interface {
	DiscoverComponentAttestations(ctx context.Context, component, version string, opts ...AttestationOption) ([]Attestation, error)
}
