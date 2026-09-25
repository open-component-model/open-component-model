// Package handler implements OpenPGP (GPG) signing and verification for OCM.
// It supports passphrase-protected private keys via the credential map.
// Signatures are stored as ASCII-armored OpenPGP detached signatures.
//
// In FIPS 140-3 mode (crypto/fips140.Enabled) Sign and Verify delegate to the
// GnuPG gpg binary on PATH so that all OpenPGP cryptography, including
// passphrase unwrapping, runs in libgcrypt; see ADR 0030.
package handler

import (
	"bytes"
	"context"
	gocrypto "crypto"
	"crypto/fips140"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

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
)

// Handler implements OpenPGP signing and verification.
type Handler struct {
	fipsEnabled func() bool
	gpgBinary   *gpgbinary.Binary
}

// New returns a Handler.
func New(_ *runtime.Scheme) (*Handler, error) {
	return &Handler{fipsEnabled: fips140.Enabled, gpgBinary: gpgbinary.New()}, nil
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

	var typedCreds *gpgcredentialsv1.GPGCredentials
	if creds != nil {
		var err error
		typedCreds, err = gpgcredentialsv1.ConvertToGPGCredentials(creds)
		if err != nil {
			return descruntime.SignatureInfo{}, fmt.Errorf("parse GPG credentials: %w", err)
		}
	}

	if h.fipsEnabled() {
		return h.signWithGPGBinary(ctx, unsigned, &sigCfg, typedCreds)
	}

	keyring, err := gpgcredentials.PrivateKeyRingFromCredentials(typedCreds)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("load GPG private key: %w", err)
	}
	if len(keyring) == 0 {
		return descruntime.SignatureInfo{}, ErrMissingPrivateKey
	}

	entity := keyring[0]
	if fp := sigCfg.GetKeyFingerprint(); fp != "" {
		entity, err = selectEntityByFingerprint(keyring, fp)
		if err != nil {
			return descruntime.SignatureInfo{}, err
		}
	}

	digestBytes, err := parseDigest(unsigned)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}

	pktCfg, err := packetConfigForHash(sigCfg.GetHashAlgorithm())
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}
	var sigBuf bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&sigBuf, entity, bytes.NewReader(digestBytes), pktCfg); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("gpg sign: %w", err)
	}

	return descruntime.SignatureInfo{
		Algorithm: v1alpha1.AlgorithmGPG,
		MediaType: v1alpha1.MediaTypeGPG,
		Value:     sigBuf.String(),
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

	var typedCreds *gpgcredentialsv1.GPGCredentials
	if creds != nil {
		var err error
		typedCreds, err = gpgcredentialsv1.ConvertToGPGCredentials(creds)
		if err != nil {
			return fmt.Errorf("parse GPG credentials: %w", err)
		}
	}

	if h.fipsEnabled() {
		return h.verifyWithGPGBinary(ctx, signed, &sigCfg, typedCreds)
	}

	keyring, err := gpgcredentials.PublicKeyRingFromCredentials(typedCreds)
	if err != nil {
		return fmt.Errorf("load GPG public key: %w", err)
	}
	if len(keyring) == 0 {
		return ErrMissingPublicKey
	}

	if fp := sigCfg.GetKeyFingerprint(); fp != "" {
		entity, err := selectEntityByFingerprint(keyring, fp)
		if err != nil {
			return err
		}
		keyring = openpgp.EntityList{entity}
	}

	digestBytes, err := parseDigest(signed.Digest)
	if err != nil {
		return err
	}

	_, err = openpgp.CheckArmoredDetachedSignature(
		keyring,
		bytes.NewReader(digestBytes),
		bytes.NewReader([]byte(signed.Signature.Value)),
		nil,
	)
	if err != nil {
		return fmt.Errorf("gpg verify: %w", err)
	}
	return nil
}

// signWithGPGBinary signs with the system gpg binary so the cryptography runs in libgcrypt.
func (h *Handler) signWithGPGBinary(
	ctx context.Context,
	unsigned descruntime.Digest,
	sigCfg *v1alpha1.Config,
	typedCreds *gpgcredentialsv1.GPGCredentials,
) (descruntime.SignatureInfo, error) {
	keyBytes, err := gpgcredentials.PrivateKeyBytes(typedCreds)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("load GPG private key: %w", err)
	}
	if len(keyBytes) == 0 {
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
	var passphrase string
	if typedCreds != nil {
		passphrase = typedCreds.Passphrase
	}

	slog.DebugContext(ctx, "FIPS 140-3 mode: signing with the system gpg binary")
	sig, err := h.gpgBinary.Sign(ctx, gpgbinary.SignRequest{
		PrivateKey:     keyBytes,
		Passphrase:     passphrase,
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

// verifyWithGPGBinary verifies with the system gpg binary so the cryptography runs in libgcrypt.
func (h *Handler) verifyWithGPGBinary(
	ctx context.Context,
	signed descruntime.Signature,
	sigCfg *v1alpha1.Config,
	typedCreds *gpgcredentialsv1.GPGCredentials,
) error {
	keyBytes, err := gpgcredentials.PublicKeyBytes(typedCreds)
	if err != nil {
		return fmt.Errorf("load GPG public key: %w", err)
	}
	if len(keyBytes) == 0 {
		return ErrMissingPublicKey
	}
	digestBytes, err := parseDigest(signed.Digest)
	if err != nil {
		return err
	}

	slog.DebugContext(ctx, "FIPS 140-3 mode: verifying with the system gpg binary")
	if err := h.gpgBinary.Verify(ctx, gpgbinary.VerifyRequest{
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

// packetConfigForHash maps a HashAlgorithm to an openpgp packet.Config.
// Returns an error for unknown or misspelled values so callers don't silently get SHA-256.
func packetConfigForHash(alg v1alpha1.HashAlgorithm) (*packet.Config, error) {
	switch alg {
	case "", v1alpha1.HashAlgorithmSHA256:
		return &packet.Config{DefaultHash: gocrypto.SHA256}, nil
	case v1alpha1.HashAlgorithmSHA384:
		return &packet.Config{DefaultHash: gocrypto.SHA384}, nil
	case v1alpha1.HashAlgorithmSHA512:
		return &packet.Config{DefaultHash: gocrypto.SHA512}, nil
	default:
		return nil, fmt.Errorf("unsupported GPG hash algorithm %q", alg)
	}
}

// gpgDigestAlgoForHash maps a HashAlgorithm to a gpg --digest-algo name.
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

// selectEntityByFingerprint finds the entity whose primary key fingerprint or
// long key ID (last 8 bytes) matches fp (case-insensitive hex).
func selectEntityByFingerprint(keyring openpgp.EntityList, fp string) (*openpgp.Entity, error) {
	upper := strings.ToUpper(fp)
	for _, e := range keyring {
		full := fmt.Sprintf("%X", e.PrimaryKey.Fingerprint)
		keyID := fmt.Sprintf("%016X", e.PrimaryKey.KeyId)
		if full == upper || keyID == upper {
			return e, nil
		}
	}
	return nil, fmt.Errorf("no key matching fingerprint %q found in keyring", fp)
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

func (h *Handler) GetCredentialTypeScheme() *runtime.Scheme {
	return gpgcredentialsspec.Scheme
}
