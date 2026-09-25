// Package handler implements OpenPGP (GPG) signing and verification for OCM.
// It supports passphrase-protected private keys via the credential map.
// Signatures are stored as ASCII-armored OpenPGP detached signatures.
//
// Sign and Verify delegate to the GnuPG gpg binary on PATH, so all OpenPGP
// cryptography, including passphrase unwrapping, runs in libgcrypt. Operated
// with a FIPS 140-3 validated libgcrypt this keeps GPG signing compliant; see ADR 0030.
package handler

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	gpgcredentials "ocm.software/open-component-model/bindings/go/gpg/signing/handler/internal/credentials"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler/internal/gpgbinary"
	gpgcredentialsspec "ocm.software/open-component-model/bindings/go/gpg/spec/credentials"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	identityv1 "ocm.software/open-component-model/bindings/go/gpg/spec/identity/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Common errors for callers to test.
var (
	ErrMissingPrivateKey = errors.New("private key not found in credentials")
	ErrMissingPublicKey  = errors.New("public key not found in credentials")
	ErrMissingHashAlg    = errors.New("missing hash algorithm in digest")
	ErrMissingDigestVal  = errors.New("missing digest value")
	// ErrKeyMaterialWithKeyring rejects credentials carrying keys when the keyring is used,
	// so that it is never ambiguous which key signs or verifies.
	ErrKeyMaterialWithKeyring = errors.New("useKeyring takes keys from the GnuPG keyring; remove the key material from the GPG credentials")
	// ErrKeyringRequiresFingerprint is returned when verifying against the keyring without a full key fingerprint.
	ErrKeyringRequiresFingerprint = gpgbinary.ErrKeyringRequiresFingerprint
)

// defaultGPGBinary serves zero-value Handlers.
var defaultGPGBinary = gpgbinary.New()

// Handler implements OpenPGP signing and verification.
// The zero value is usable and behaves like a Handler returned by New.
type Handler struct {
	gpgBinary *gpgbinary.Binary // nil means defaultGPGBinary
}

// New returns a Handler.
func New(_ *runtime.Scheme) (*Handler, error) {
	return &Handler{gpgBinary: gpgbinary.New()}, nil
}

func (h *Handler) binary() *gpgbinary.Binary {
	if h.gpgBinary == nil {
		return defaultGPGBinary
	}
	return h.gpgBinary
}

// GetSigningHandlerScheme returns the scheme for this handler's config types.
func (h *Handler) GetSigningHandlerScheme() *runtime.Scheme {
	return v1alpha1.Scheme
}

// Sign produces an ASCII-armored OpenPGP detached signature over the digest bytes.
func (h *Handler) Sign(
	ctx context.Context,
	unsigned descruntime.Digest,
	cfg runtime.Typed,
	creds runtime.Typed,
) (descruntime.SignatureInfo, error) {
	var sigCfg v1alpha1.Config
	if err := h.GetSigningHandlerScheme().Convert(cfg, &sigCfg); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("convert config: %w", err)
	}

	typedCreds, err := convertCredentials(creds)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}

	keyBytes, err := gpgcredentials.PrivateKeyBytes(typedCreds)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("load GPG private key: %w", err)
	}
	switch {
	case sigCfg.UseKeyring && len(keyBytes) > 0:
		return descruntime.SignatureInfo{}, ErrKeyMaterialWithKeyring
	case !sigCfg.UseKeyring && len(keyBytes) == 0:
		return descruntime.SignatureInfo{}, ErrMissingPrivateKey
	}
	digestBytes, err := parseDigest(unsigned)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}
	algo, err := gpgDigestAlgoForHash(sigCfg.GetHashAlgorithm())
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}

	slog.DebugContext(ctx, "signing with the system gpg binary")
	sig, err := h.binary().Sign(ctx, gpgbinary.SignRequest{
		UseKeyring:     sigCfg.UseKeyring,
		PrivateKey:     keyBytes,
		Passphrase:     typedCreds.Passphrase,
		KeyFingerprint: sigCfg.GetKeyFingerprint(),
		DigestAlgo:     algo,
		Data:           digestBytes,
	})
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("gpg sign: %w", err)
	}
	return descruntime.SignatureInfo{
		Algorithm: v1alpha1.AlgorithmGPG,
		MediaType: v1alpha1.MediaTypeGPG,
		Value:     sig,
	}, nil
}

// Verify validates an OpenPGP detached signature stored in SignatureInfo.Value.
func (h *Handler) Verify(
	ctx context.Context,
	signed descruntime.Signature,
	cfg runtime.Typed,
	creds runtime.Typed,
) error {
	if signed.Signature.MediaType != v1alpha1.MediaTypeGPG {
		return fmt.Errorf("unsupported media type %q for GPG verification", signed.Signature.MediaType)
	}

	var sigCfg v1alpha1.Config
	if err := h.GetSigningHandlerScheme().Convert(cfg, &sigCfg); err != nil {
		return fmt.Errorf("convert config: %w", err)
	}

	typedCreds, err := convertCredentials(creds)
	if err != nil {
		return err
	}

	keyBytes, err := gpgcredentials.PublicKeyBytes(typedCreds)
	if err != nil {
		return fmt.Errorf("load GPG public key: %w", err)
	}
	switch {
	case sigCfg.UseKeyring && len(keyBytes) > 0:
		return ErrKeyMaterialWithKeyring
	case !sigCfg.UseKeyring && len(keyBytes) == 0:
		return ErrMissingPublicKey
	}
	digestBytes, err := parseDigest(signed.Digest)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "verifying with the system gpg binary")
	if err := h.binary().Verify(ctx, gpgbinary.VerifyRequest{
		UseKeyring:     sigCfg.UseKeyring,
		PublicKey:      keyBytes,
		KeyFingerprint: sigCfg.GetKeyFingerprint(),
		Data:           digestBytes,
		Signature:      signed.Signature.Value,
	}); err != nil {
		return fmt.Errorf("gpg verify: %w", err)
	}
	return nil
}

// GetSigningCredentialConsumerIdentity returns the credential consumer identity for signing.
func (*Handler) GetSigningCredentialConsumerIdentity(
	_ context.Context,
	name string,
	_ descruntime.Digest,
	_ runtime.Typed,
) (runtime.Identity, error) {
	id := baseIdentity()
	id.Signature = name
	return gpgIdentityToMap(id), nil
}

// GetVerifyingCredentialConsumerIdentity returns the credential consumer identity for verification.
func (*Handler) GetVerifyingCredentialConsumerIdentity(
	_ context.Context,
	signed descruntime.Signature,
	_ runtime.Typed,
) (runtime.Identity, error) {
	id := baseIdentity()
	id.Signature = signed.Name
	return gpgIdentityToMap(id), nil
}

func (h *Handler) GetCredentialTypeScheme() *runtime.Scheme {
	return gpgcredentialsspec.Scheme
}

// convertCredentials returns the typed credentials; nil creds yield empty credentials.
func convertCredentials(creds runtime.Typed) (*gpgcredentialsv1.GPGCredentials, error) {
	if creds == nil {
		return &gpgcredentialsv1.GPGCredentials{}, nil
	}
	typed, err := gpgcredentialsv1.ConvertToGPGCredentials(creds)
	if err != nil {
		return nil, fmt.Errorf("parse GPG credentials: %w", err)
	}
	return typed, nil
}

func baseIdentity() *identityv1.GPGIdentity {
	return &identityv1.GPGIdentity{
		Type: identityv1.V1Alpha1Type,
	}
}

// gpgIdentityToMap converts a typed GPGIdentity to a runtime.Identity map.
func gpgIdentityToMap(id *identityv1.GPGIdentity) runtime.Identity {
	m := runtime.Identity{
		identityv1.IdentityAttributeSignature: id.Signature,
	}
	m.SetType(id.Type)
	return m
}

// gpgDigestAlgoForHash maps a HashAlgorithm to a gpg --digest-algo name.
// Returns an error for unknown or misspelled values so callers don't silently get SHA-256.
func gpgDigestAlgoForHash(alg v1alpha1.HashAlgorithm) (string, error) {
	switch alg {
	case "", v1alpha1.HashAlgorithmSHA256:
		return "SHA256", nil
	case v1alpha1.HashAlgorithmSHA384:
		return "SHA384", nil
	case v1alpha1.HashAlgorithmSHA512:
		return "SHA512", nil
	default:
		return "", fmt.Errorf("unsupported GPG hash algorithm %q", alg)
	}
}

// parseDigest validates and hex-decodes the digest value.
func parseDigest(d descruntime.Digest) ([]byte, error) {
	if d.HashAlgorithm == "" {
		return nil, ErrMissingHashAlg
	}
	if d.Value == "" {
		return nil, ErrMissingDigestVal
	}
	if err := validateHashAlgorithm(d.HashAlgorithm); err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(d.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid hex digest: %w", err)
	}
	return b, nil
}

func validateHashAlgorithm(alg string) error {
	switch alg {
	case "SHA-256", "SHA-384", "SHA-512":
		return nil
	}
	return fmt.Errorf("unsupported hash algorithm %q", alg)
}
