// Package handler implements Notation signing and verification for OCM using
// the Notary Project's notation-go blob API, fully in-process.
//
// Signing takes OCM's precomputed descriptor digest (hex), feeds the decoded
// bytes to notation.SignBlob using a private key and X.509 certificate chain
// resolved from credentials, and stores the resulting signature envelope
// (JWS "application/jose+json" by default, or COSE "application/cose") as the
// base64-encoded SignatureInfo.Value.
//
// Verification decodes the same digest bytes and the envelope, builds an
// in-memory trust store from CA certificates supplied via credentials plus an
// in-code blob trust policy, and calls notation.VerifyBlob. Verification
// confirms the envelope binds the re-hashed bytes and that the signing chain
// terminates at a trusted CA and matches the configured trusted identities.
//
// Unlike the Sigstore handler, this handler does not shell out to an external
// binary and does not read notation's on-disk configuration; all key and trust
// material comes from OCM credentials.
package handler

import (
	"bytes"
	"context"
	"crypto"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	// Register the hash implementations that notation-go's blob descriptor
	// generator selects from the signing key. crypto/sha512 provides both
	// SHA-384 and SHA-512.
	_ "crypto/sha256"
	_ "crypto/sha512"

	"github.com/notaryproject/notation-core-go/signature/cose"
	"github.com/notaryproject/notation-core-go/signature/jws"
	"github.com/notaryproject/notation-go"
	notationsigner "github.com/notaryproject/notation-go/signer"
	"github.com/notaryproject/notation-go/verifier"
	"github.com/notaryproject/notation-go/verifier/trustpolicy"
	"github.com/opencontainers/go-digest"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/notation/signing/handler/internal"
	notationcreds "ocm.software/open-component-model/bindings/go/notation/signing/handler/internal/credentials"
	"ocm.software/open-component-model/bindings/go/notation/signing/v1alpha1"
	notationcredsv1 "ocm.software/open-component-model/bindings/go/notation/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/notation/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

var _ signing.Handler = (*Handler)(nil)

func init() {
	// go-digest does not register SHA-384 by default (it is not part of the OCI
	// image spec). notation-go's blob signer selects SHA-384 for 3072-bit RSA
	// keys, so register it here to avoid a runtime panic in the descriptor
	// generator. Idempotent: RegisterAlgorithm is safe to call repeatedly.
	digest.RegisterAlgorithm(digest.SHA384, crypto.SHA384)
}

// Common errors for callers to test.
var (
	// ErrMissingPrivateKey is returned by Sign when no signing key is available.
	ErrMissingPrivateKey = errors.New("private key not found")
	// ErrMissingCertificateChain is returned by Sign when no certificate chain is available.
	ErrMissingCertificateChain = errors.New("certificate chain not found")
	// ErrMissingTrustedCACertificates is returned by Verify when no trust anchor is available.
	ErrMissingTrustedCACertificates = errors.New("trusted CA certificates not found")
)

// trustPolicyName is the fixed statement name used in the in-code blob trust
// policy and referenced by verify options.
const trustPolicyName = "ocm"

// Handler implements signing.Handler using the notation-go blob API.
// Safe for concurrent use.
type Handler struct{}

// New creates a Handler.
func New() *Handler {
	return &Handler{}
}

// GetSigningHandlerScheme returns the runtime.Scheme containing registered config types.
func (h *Handler) GetSigningHandlerScheme() *runtime.Scheme {
	return v1alpha1.Scheme
}

// Sign signs the descriptor digest via notation.SignBlob using the private key
// and certificate chain resolved from credentials, and returns the base64
// signature envelope.
func (h *Handler) Sign(
	ctx context.Context,
	unsigned descruntime.Digest,
	rawCfg runtime.Typed,
	creds runtime.Typed,
) (descruntime.SignatureInfo, error) {
	var cfg v1alpha1.SignConfig
	if err := v1alpha1.Scheme.Convert(rawCfg, &cfg); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("convert config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("invalid signing config: %w", err)
	}
	algorithm := cfg.GetSignatureAlgorithm()
	envelopeMediaType := cfg.GetEnvelopeMediaType()

	digestBytes, err := hex.DecodeString(unsigned.Value)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("decode digest hex value: %w", err)
	}
	if len(digestBytes) == 0 {
		return descruntime.SignatureInfo{}, fmt.Errorf("digest value must not be empty")
	}

	var notationCreds *notationcredsv1.NotationCredentials
	if creds != nil {
		notationCreds, err = notationcredsv1.ConvertToNotationCredentials(creds)
		if err != nil {
			return descruntime.SignatureInfo{}, fmt.Errorf("convert credentials for signing: %w", err)
		}
	}

	privateKey, err := notationcreds.PrivateKeyFromCredentials(notationCreds)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("load private key: %w", err)
	}
	if privateKey == nil {
		return descruntime.SignatureInfo{}, ErrMissingPrivateKey
	}

	chain, err := notationcreds.CertificateChainFromCredentials(notationCreds)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("load certificate chain: %w", err)
	}
	if len(chain) == 0 {
		return descruntime.SignatureInfo{}, ErrMissingCertificateChain
	}

	blobSigner, err := notationsigner.NewGenericSigner(privateKey, chain)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("create notation signer: %w", err)
	}

	opts := notation.SignBlobOptions{
		SignerSignOptions: notation.SignerSignOptions{
			SignatureMediaType: notationEnvelopeMediaType(envelopeMediaType),
			ExpiryDuration:     cfg.GetExpiryDuration(),
		},
		ContentMediaType: contentMediaType,
	}

	envelope, _, err := notation.SignBlob(ctx, blobSigner, bytes.NewReader(digestBytes), opts)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("notation sign blob: %w", err)
	}

	return descruntime.SignatureInfo{
		Algorithm: string(algorithm),
		MediaType: envelopeMediaType,
		Value:     base64.StdEncoding.EncodeToString(envelope),
	}, nil
}

// Verify checks a Notation blob signature via notation.VerifyBlob against an
// in-memory trust store built from credential-supplied CA certificates.
func (h *Handler) Verify(
	ctx context.Context,
	signed descruntime.Signature,
	rawCfg runtime.Typed,
	creds runtime.Typed,
) error {
	var cfg v1alpha1.VerifyConfig
	if rawCfg != nil {
		if err := v1alpha1.Scheme.Convert(rawCfg, &cfg); err != nil {
			return fmt.Errorf("convert config: %w", err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid verification config: %w", err)
	}

	if err := validateSignatureEnvelope(signed.Signature); err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	digestBytes, err := hex.DecodeString(signed.Digest.Value)
	if err != nil {
		return fmt.Errorf("decode digest hex: %w", err)
	}
	if len(digestBytes) == 0 {
		return fmt.Errorf("digest value must not be empty")
	}

	envelope, err := base64.StdEncoding.DecodeString(signed.Signature.Value)
	if err != nil {
		return fmt.Errorf("decode signature envelope base64: %w", err)
	}

	var notationCreds *notationcredsv1.NotationCredentials
	if creds != nil {
		notationCreds, err = notationcredsv1.ConvertToNotationCredentials(creds)
		if err != nil {
			return fmt.Errorf("convert credentials for verify: %w", err)
		}
	}
	caCerts, err := notationcreds.TrustedCACertificatesFromCredentials(notationCreds)
	if err != nil {
		return fmt.Errorf("load trusted CA certificates: %w", err)
	}
	if len(caCerts) == 0 {
		return ErrMissingTrustedCACertificates
	}

	trustStore := internal.NewInMemoryTrustStore(caCerts)
	policy := blobTrustPolicy(cfg)

	blobVerifier, err := verifier.NewVerifierWithOptions(trustStore, verifier.VerifierOptions{
		BlobTrustPolicy: policy,
	})
	if err != nil {
		return fmt.Errorf("create notation verifier: %w", err)
	}

	opts := notation.VerifyBlobOptions{
		BlobVerifierVerifyOptions: notation.BlobVerifierVerifyOptions{
			SignatureMediaType: notationEnvelopeMediaType(signed.Signature.MediaType),
			TrustPolicyName:    trustPolicyName,
		},
		ContentMediaType: contentMediaType,
	}

	if _, _, err := notation.VerifyBlob(ctx, blobVerifier, bytes.NewReader(digestBytes), envelope, opts); err != nil {
		return fmt.Errorf("notation verify blob: %w", err)
	}
	return nil
}

func (*Handler) GetSigningCredentialConsumerIdentity(
	_ context.Context,
	name string,
	_ descruntime.Digest,
	_ runtime.Typed,
) (runtime.Identity, error) {
	id := runtime.Identity{}
	id.SetType(identityv1.VersionedType)
	id[identityv1.IdentityAttributeSignature] = name
	return id, nil
}

func (*Handler) GetVerifyingCredentialConsumerIdentity(
	_ context.Context,
	signature descruntime.Signature,
	_ runtime.Typed,
) (runtime.Identity, error) {
	if err := validateSignatureEnvelope(signature.Signature); err != nil {
		return nil, fmt.Errorf("verifying credential identity: %w", err)
	}
	id := runtime.Identity{}
	id.SetType(identityv1.VersionedType)
	id[identityv1.IdentityAttributeSignature] = signature.Name
	return id, nil
}

// contentMediaType is the blob content media type declared to notation. The
// value is opaque to OCM: signing and verification use the same constant so the
// descriptor the envelope binds is reproducible.
const contentMediaType = "application/octet-stream"

// notationEnvelopeMediaType maps the OCM-facing envelope media type to the
// notation-go envelope constant. It falls back to JWS for any unexpected value;
// callers validate the media type before reaching this point.
func notationEnvelopeMediaType(mediaType string) string {
	switch mediaType {
	case v1alpha1.MediaTypeCOSEEnvelope:
		return cose.MediaTypeEnvelope
	default:
		return jws.MediaTypeEnvelope
	}
}

// validateSignatureEnvelope checks the signature carries the known OCM Notation
// algorithm and an acceptable envelope MediaType. Empty Algorithm is rejected
// (not defaulted): an OCM signature without a declared algorithm is foreign or
// malformed.
func validateSignatureEnvelope(sig descruntime.SignatureInfo) error {
	if sig.Algorithm == "" {
		return fmt.Errorf("signature.Algorithm is required for notation verification")
	}
	if v1alpha1.SignatureAlgorithm(sig.Algorithm) != v1alpha1.AlgorithmNotationV1Alpha1 {
		return fmt.Errorf("%w: %q", v1alpha1.ErrUnknownAlgorithm, sig.Algorithm)
	}
	switch sig.MediaType {
	case v1alpha1.MediaTypeJWSEnvelope, v1alpha1.MediaTypeCOSEEnvelope:
	default:
		return fmt.Errorf("unsupported media type %q for notation verification", sig.MediaType)
	}
	return nil
}

// blobTrustPolicy builds a single global blob trust policy from the verify
// config. Revocation is skipped: OCM verification is offline and must not
// depend on OCSP/CRL network reachability. Integrity, authenticity, and
// certificate-chain validation remain enforced at the selected level.
func blobTrustPolicy(cfg v1alpha1.VerifyConfig) *trustpolicy.BlobDocument {
	return &trustpolicy.BlobDocument{
		Version: "1.0",
		TrustPolicies: []trustpolicy.BlobTrustPolicy{
			{
				Name: trustPolicyName,
				SignatureVerification: trustpolicy.SignatureVerification{
					VerificationLevel: cfg.GetVerificationLevel(),
					Override: map[trustpolicy.ValidationType]trustpolicy.ValidationAction{
						trustpolicy.TypeRevocation: trustpolicy.ActionSkip,
					},
				},
				TrustStores:       []string{"ca:" + internal.NamedStore},
				TrustedIdentities: cfg.GetTrustedIdentities(),
				GlobalPolicy:      true,
			},
		},
	}
}
