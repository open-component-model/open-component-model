// Package handler implements OpenPGP (GPG) signing and verification for OCM.
// Signatures are ASCII-armored OpenPGP detached signatures over the digest bytes.
//
// All OpenPGP cryptography runs in-process in pure Go (github.com/ProtonMail/go-crypto), whose RSA,
// NIST-curve ECDSA, SHA-2 and random number generation run in the Go Cryptographic Module. The key
// material comes from the credentials, or, with keyringFingerprint, is exported from the user's GnuPG
// keyring with the gpg binary, which performs no signing or verification.
//
// Steps outside the module (keys other than v6, algorithms other than RSA and ECDSA on NIST curves,
// unlocking passphrase-protected keys, exporting secret keys from the GnuPG keyring) follow the FIPS
// 140-3 mode rules of ADR 0030: with GODEBUG=fips140=on they are logged at debug level, with
// GODEBUG=fips140=only they are rejected.
package handler

import (
	"bytes"
	"cmp"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

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
	ErrNoSecretKey       = errors.New("no secret key found in private key material")
	ErrMissingPassphrase = errors.New("private key is passphrase-protected but no passphrase was provided")
	// ErrGPGNotFound is returned when keys are to be exported from the GnuPG keyring but no gpg binary is found on PATH.
	ErrGPGNotFound = gpgbinary.ErrGPGNotFound
	// ErrKeyMaterialWithKeyring rejects credentials carrying key material next to keyringFingerprint,
	// so that it is never ambiguous which key signs or verifies.
	ErrKeyMaterialWithKeyring = errors.New("keyringFingerprint takes the keys from the GnuPG keyring; remove privateKeyPGP, privateKeyPGPFile, publicKeyPGP and publicKeyPGPFile from the GPG credentials")
	// ErrKeyringRequiresFingerprint is returned when keyringFingerprint is not a full key fingerprint.
	ErrKeyringRequiresFingerprint = errors.New("keyringFingerprint must be a full key fingerprint (40 or 64 hex characters), because a shorter key ID could select another key in the keyring")
	// ErrHashTooShortForKey is returned when the configured hashAlgorithm is shorter than the signing key needs.
	ErrHashTooShortForKey = errors.New("hashAlgorithm is too short for the signing key")

	// Strict FIPS 140-3 mode (GODEBUG=fips140=only) errors.

	ErrV4KeyInFIPSMode            = errors.New("with GODEBUG=fips140=only, GPG signing and verification require OpenPGP v6 (RFC 9580) or v5 (LibrePGP) keys: v4 and older keys are identified by SHA-1 fingerprints")
	ErrAlgorithmInFIPSMode        = errors.New("with GODEBUG=fips140=only, GPG signing and verification require an RSA or ECDSA (P-256, P-384, P-521) key")
	ErrProtectedKeyInFIPSMode     = errors.New("with GODEBUG=fips140=only, passphrase-protected GPG keys cannot be unlocked because OpenPGP key protection is not FIPS-approved; provide the private key without passphrase protection, for example from a secret store")
	ErrKeyringSecretKeyInFIPSMode = errors.New("with GODEBUG=fips140=only, secret keys cannot be exported from the GnuPG keyring because gpg-agent processes them outside the Go Cryptographic Module; provide the private key in the GPG credentials")
)

// defaultGPGBinary serves zero-value Handlers. Its configuration is immutable; it only caches the resolved path.
var defaultGPGBinary = gpgbinary.New()

// Handler implements OpenPGP signing and verification.
// The zero value is usable and behaves like a Handler returned by New.
type Handler struct {
	gpgBinary *gpgbinary.Binary // nil means defaultGPGBinary
}

// New returns a Handler.
func New(_ *runtime.Scheme) (*Handler, error) {
	return &Handler{}, nil
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
	material, keyringFpr, err := h.privateKeyMaterial(ctx, typedCreds)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}
	if len(material) == 0 {
		return descruntime.SignatureInfo{}, ErrMissingPrivateKey
	}
	digestBytes, err := parseDigest(unsigned)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}
	configuredHash, err := hashForAlgorithm(sigCfg.HashAlgorithm)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}

	entities, err := readKeyRing(ctx, material)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("load GPG private key: %w", err)
	}
	k, err := selectSigningKey(entities, cmp.Or(sigCfg.GetKeyFingerprint(), keyringFpr), time.Now())
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}
	hash, err := signatureHash(configuredHash, k.PublicKey)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}
	if k.PrivateKey.Encrypted {
		if err := fipsBoundary(ctx, ErrProtectedKeyInFIPSMode, "unlocking a passphrase-protected GPG key"); err != nil {
			return descruntime.SignatureInfo{}, err
		}
		if typedCreds.Passphrase == "" {
			return descruntime.SignatureInfo{}, ErrMissingPassphrase
		}
		if err := k.PrivateKey.Decrypt([]byte(typedCreds.Passphrase)); err != nil {
			return descruntime.SignatureInfo{}, fmt.Errorf("decrypt GPG private key: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&buf, k.Entity, bytes.NewReader(digestBytes), &packet.Config{
		DefaultHash:  hash,
		SigningKeyId: k.PublicKey.KeyId,
	}); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("gpg sign failed: %w", err)
	}
	return descruntime.SignatureInfo{
		Algorithm: v1alpha1.AlgorithmGPG,
		MediaType: v1alpha1.MediaTypeGPG,
		Value:     buf.String(),
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
	material, keyringFpr, err := h.publicKeyMaterial(ctx, typedCreds)
	if err != nil {
		return err
	}
	if len(material) == 0 {
		return ErrMissingPublicKey
	}
	digestBytes, err := parseDigest(signed.Digest)
	if err != nil {
		return err
	}

	keyring, err := readKeyRing(ctx, material)
	if err != nil {
		return fmt.Errorf("load GPG public key: %w", err)
	}
	sig, body, err := parseSignature(signed.Signature.Value)
	if err != nil {
		return err
	}

	// A nil config verifies at time.Now(): key and subkey expiry, revocations and the expiry of the binding and
	// message signatures are all checked at that one time, so signatures stop verifying once their key expires or
	// is retired. With TSA support, pass the verified timestamp token's genTime as packet.Config.Time instead; go-crypto
	// v1 then runs every check at that time, and a "key compromised" revocation still applies at any date.
	// Never use sig.CreationTime: the signer sets it and could backdate a signature into an expired key's validity.
	// openpgp/v2 is not a substitute: it judges the key at sig.CreationTime regardless of Config.Time.
	_, signer, err := openpgp.VerifyDetachedSignature(keyring, bytes.NewReader(digestBytes), bytes.NewReader(body), nil)
	if err != nil {
		return fmt.Errorf("gpg verify failed: %w", err)
	}

	want := cmp.Or(sigCfg.GetKeyFingerprint(), keyringFpr)
	if want == "" {
		return nil
	}
	signing := signer.PrimaryKey
	for _, s := range signer.Subkeys {
		if s.PublicKey.KeyId == *sig.IssuerKeyId {
			signing = s.PublicKey
			break
		}
	}
	if matchesFingerprint(signing, want) || matchesFingerprint(signer.PrimaryKey, want) {
		return nil
	}
	return fmt.Errorf("signature was made by key %X (primary key %X), which does not match the configured key fingerprint %q",
		signing.Fingerprint, signer.PrimaryKey.Fingerprint, want)
}

// privateKeyMaterial returns the private key material of the credentials, or the secret key exported from the
// GnuPG keyring together with its normalized keyring fingerprint.
func (h *Handler) privateKeyMaterial(ctx context.Context, creds *gpgcredentialsv1.GPGCredentials) (material []byte, keyringFpr string, err error) {
	if creds.KeyringFingerprint == "" {
		material, err := gpgcredentials.PrivateKeyBytes(creds)
		if err != nil {
			return nil, "", fmt.Errorf("load GPG private key: %w", err)
		}
		return material, "", nil
	}
	fpr, err := keyringFingerprint(creds)
	if err != nil {
		return nil, "", err
	}
	if err := fipsBoundary(ctx, ErrKeyringSecretKeyInFIPSMode, "exporting a secret key from the GnuPG keyring"); err != nil {
		return nil, "", err
	}
	material, err = h.binary().ExportSecretKey(ctx, creds.KeyringHome, fpr, creds.Passphrase)
	if err != nil {
		return nil, "", fmt.Errorf("export GPG private key from the GnuPG keyring: %w", err)
	}
	return material, fpr, nil
}

// publicKeyMaterial returns the public key material of the credentials, or the public key exported from the
// GnuPG keyring together with its normalized keyring fingerprint.
func (h *Handler) publicKeyMaterial(ctx context.Context, creds *gpgcredentialsv1.GPGCredentials) (material []byte, keyringFpr string, err error) {
	if creds.KeyringFingerprint == "" {
		material, err := gpgcredentials.PublicKeyBytes(creds)
		if err != nil {
			return nil, "", fmt.Errorf("load GPG public key: %w", err)
		}
		return material, "", nil
	}
	fpr, err := keyringFingerprint(creds)
	if err != nil {
		return nil, "", err
	}
	material, err = h.binary().ExportPublicKey(ctx, creds.KeyringHome, fpr)
	if err != nil {
		return nil, "", fmt.Errorf("export GPG public key from the GnuPG keyring: %w", err)
	}
	return material, fpr, nil
}

// keyringFingerprint validates the keyring credentials and returns the normalized keyring fingerprint.
func keyringFingerprint(creds *gpgcredentialsv1.GPGCredentials) (string, error) {
	if creds.PrivateKeyPGP != "" || creds.PrivateKeyPGPFile != "" || creds.PublicKeyPGP != "" || creds.PublicKeyPGPFile != "" {
		return "", ErrKeyMaterialWithKeyring
	}
	fpr := v1alpha1.NormalizeFingerprint(creds.KeyringFingerprint)
	if len(fpr) != 40 && len(fpr) != 64 {
		return "", ErrKeyringRequiresFingerprint
	}
	if _, err := hex.DecodeString(fpr); err != nil {
		return "", ErrKeyringRequiresFingerprint
	}
	return fpr, nil
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
