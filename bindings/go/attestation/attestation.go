// Package attestation builds cosign-verifiable in-toto attestations and packages
// them as Sigstore bundles.
//
// The output is a Sigstore bundle (media type [MediaTypeBundle]) wrapping a DSSE
// envelope over an in-toto statement, signed with an ECDSA P-256 key. When
// stored as an OCI referrer of a manifest whose digest matches the statement
// subject, the attestation is verifiable offline and key-based with:
//
//	cosign verify-attestation --key <pub> --type <predicate> --insecure-ignore-tlog <ref>
//
// It intentionally depends only on the DSSE library and the standard library,
// not on the full sigstore/cosign module tree.
package attestation

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/secure-systems-lab/go-securesystemslib/dsse"
)

const (
	// MediaTypeBundle is the media type of a Sigstore bundle. It is used both as
	// the OCI referrer artifactType and as the referrer's single layer media
	// type, matching what cosign writes and expects.
	MediaTypeBundle = "application/vnd.dev.sigstore.bundle.v0.3+json"

	// MediaTypeInTotoStatement is the DSSE payload type for an in-toto statement.
	MediaTypeInTotoStatement = "application/vnd.in-toto+json"

	// StatementType is the in-toto statement type.
	StatementType = "https://in-toto.io/Statement/v0.1"
)

// Subject identifies the artifact an attestation is about. For cosign
// verification the Digest MUST match the OCI manifest digest of the referrer's
// subject.
type Subject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

// statement is the in-toto statement wrapping the predicate.
type statement struct {
	Type          string          `json:"_type"`
	PredicateType string          `json:"predicateType"`
	Subject       []Subject       `json:"subject"`
	Predicate     json.RawMessage `json:"predicate"`
}

// bundle is the minimal Sigstore bundle v0.3 shape cosign accepts for key-based,
// offline (tlog-ignored) verification.
type bundle struct {
	MediaType            string               `json:"mediaType"`
	VerificationMaterial verificationMaterial `json:"verificationMaterial"`
	DSSEEnvelope         dsseEnvelope         `json:"dsseEnvelope"`
}

type verificationMaterial struct {
	PublicKey publicKey `json:"publicKey"`
}

type publicKey struct {
	Hint string `json:"hint"`
}

type dsseEnvelope struct {
	Payload     string          `json:"payload"`
	PayloadType string          `json:"payloadType"`
	Signatures  []dsseSignature `json:"signatures"`
}

type dsseSignature struct {
	Sig string `json:"sig"`
}

// ecdsaSigner adapts an ECDSA P-256 private key to the DSSE Signer interface.
// DSSE calls Sign with the pre-authentication encoding (PAE) bytes; we sign the
// SHA-256 of those bytes, matching what cosign verifies for ECDSA P-256.
type ecdsaSigner struct {
	key *ecdsa.PrivateKey
}

func (s ecdsaSigner) Sign(_ context.Context, data []byte) ([]byte, error) {
	digest := sha256.Sum256(data)
	return ecdsa.SignASN1(rand.Reader, s.key, digest[:])
}

func (s ecdsaSigner) KeyID() (string, error) {
	// A key hint is optional for key-based verification; cosign uses the public
	// key supplied on the command line, not this identifier.
	return "", nil
}

// SignedBundle builds an in-toto statement for the given subject and predicate,
// signs it into a DSSE envelope with the ECDSA P-256 key, and returns the
// serialized Sigstore bundle together with its media type.
//
// predicateType is the SLSA/in-toto predicate type (also the value passed to
// `cosign verify-attestation --type`). predicate is the raw predicate JSON.
func SignedBundle(ctx context.Context, subject Subject, predicateType string, predicate json.RawMessage, key *ecdsa.PrivateKey) (bundleJSON []byte, mediaType string, err error) {
	if key == nil {
		return nil, "", fmt.Errorf("signing key is required")
	}
	if subject.Name == "" || len(subject.Digest) == 0 {
		return nil, "", fmt.Errorf("subject name and digest are required")
	}

	stmt := statement{
		Type:          StatementType,
		PredicateType: predicateType,
		Subject:       []Subject{subject},
		Predicate:     predicate,
	}
	payload, err := json.Marshal(stmt)
	if err != nil {
		return nil, "", fmt.Errorf("marshalling statement failed: %w", err)
	}

	signer, err := dsse.NewEnvelopeSigner(ecdsaSigner{key: key})
	if err != nil {
		return nil, "", fmt.Errorf("creating envelope signer failed: %w", err)
	}
	env, err := signer.SignPayload(ctx, MediaTypeInTotoStatement, payload)
	if err != nil {
		return nil, "", fmt.Errorf("signing attestation failed: %w", err)
	}

	sigs := make([]dsseSignature, 0, len(env.Signatures))
	for _, s := range env.Signatures {
		sigs = append(sigs, dsseSignature{Sig: s.Sig})
	}

	b := bundle{
		MediaType: MediaTypeBundle,
		DSSEEnvelope: dsseEnvelope{
			Payload:     env.Payload,
			PayloadType: env.PayloadType,
			Signatures:  sigs,
		},
	}
	bundleJSON, err = json.Marshal(b)
	if err != nil {
		return nil, "", fmt.Errorf("marshalling bundle failed: %w", err)
	}
	return bundleJSON, MediaTypeBundle, nil
}

// DigestFromManifestReference extracts the hex sha256 from a "sha256:<hex>"
// digest string, for building the statement subject.
func DigestFromManifestReference(digest string) (map[string]string, error) {
	const prefix = "sha256:"
	if len(digest) <= len(prefix) || digest[:len(prefix)] != prefix {
		return nil, fmt.Errorf("unsupported digest %q: expected sha256:<hex>", digest)
	}
	return map[string]string{"sha256": digest[len(prefix):]}, nil
}
