// Package handler implements OpenPGP (GPG) signing and verification for OCM.
// It supports passphrase-protected private keys via the credential map.
// Signatures are stored as ASCII-armored OpenPGP detached signatures.
package handler

import (
	"bytes"
	"context"
	gocrypto "crypto"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	gpgcredentials "ocm.software/open-component-model/bindings/go/gpg/signing/handler/internal/credentials"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	identityv1 "ocm.software/open-component-model/bindings/go/gpg/spec/identity/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// IdentityAttributeSignature will be removed in the future.
//
// Deprecated: Use typed identity [identityv1.GPGIdentity] instead.
const IdentityAttributeSignature = identityv1.IdentityAttributeSignature

// Common errors for callers to test.
var (
	ErrMissingPrivateKey = errors.New("private key not found in credentials")
	ErrMissingPublicKey  = errors.New("public key not found in credentials")
	ErrMissingHashAlg    = errors.New("missing hash algorithm in digest")
	ErrMissingDigestVal  = errors.New("missing digest value")
)

// Handler implements OpenPGP signing and verification.
type Handler struct{}

// New returns a Handler.
func New(_ *runtime.Scheme) (*Handler, error) {
	return &Handler{}, nil
}

// GetSigningHandlerScheme returns the scheme for this handler's config types.
func (h *Handler) GetSigningHandlerScheme() *runtime.Scheme {
	return v1alpha1.Scheme
}

// Sign produces an ASCII-armored OpenPGP detached signature over the digest bytes.
func (h *Handler) Sign(
	_ context.Context,
	unsigned descruntime.Digest,
	cfg runtime.Typed,
	creds map[string]string,
) (descruntime.SignatureInfo, error) {
	sigCfg := configFrom(cfg)
	typedCreds := gpgcredentialsv1.FromDirectCredentials(creds)

	entity, err := gpgcredentials.PrivateEntityFromCredentials(typedCreds)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("load GPG private key: %w", err)
	}
	if entity == nil {
		return descruntime.SignatureInfo{}, ErrMissingPrivateKey
	}

	if fp := sigCfg.GetKeyFingerprint(); fp != "" {
		entity, err = selectEntityByFingerprint(openpgp.EntityList{entity}, fp)
		if err != nil {
			return descruntime.SignatureInfo{}, err
		}
	}

	digestBytes, err := parseDigest(unsigned)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}

	pktCfg := packetConfigForHash(sigCfg.GetHashAlgorithm())
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
	_ context.Context,
	signed descruntime.Signature,
	cfg runtime.Typed,
	creds map[string]string,
) error {
	if signed.Signature.MediaType != v1alpha1.MediaTypeGPG {
		return fmt.Errorf("unsupported media type %q for GPG verification", signed.Signature.MediaType)
	}

	sigCfg := configFrom(cfg)
	typedCreds := gpgcredentialsv1.FromDirectCredentials(creds)

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

// configFrom extracts a *v1alpha1.Config from the typed value, falling back to defaults.
func configFrom(cfg runtime.Typed) *v1alpha1.Config {
	if c, ok := cfg.(*v1alpha1.Config); ok {
		return c
	}
	return &v1alpha1.Config{}
}

// packetConfigForHash maps a HashAlgorithm to an openpgp packet.Config.
func packetConfigForHash(alg v1alpha1.HashAlgorithm) *packet.Config {
	switch alg {
	case v1alpha1.HashAlgorithmSHA384:
		return &packet.Config{DefaultHash: gocrypto.SHA384}
	case v1alpha1.HashAlgorithmSHA512:
		return &packet.Config{DefaultHash: gocrypto.SHA512}
	default:
		return &packet.Config{DefaultHash: gocrypto.SHA256}
	}
}

// selectEntityByFingerprint finds the entity whose primary key fingerprint or
// long key ID (last 8 bytes) matches fp (case-insensitive hex).
func selectEntityByFingerprint(keyring openpgp.EntityList, fp string) (*openpgp.Entity, error) {
	for _, e := range keyring {
		full := fmt.Sprintf("%X", e.PrimaryKey.Fingerprint)
		keyID := fmt.Sprintf("%016X", e.PrimaryKey.KeyId)
		if equalFold(full, fp) || equalFold(keyID, fp) {
			return e, nil
		}
	}
	return nil, fmt.Errorf("no key matching fingerprint %q found in keyring", fp)
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if ca >= 'a' && ca <= 'f' {
			ca -= 32
		}
		if cb >= 'a' && cb <= 'f' {
			cb -= 32
		}
		if ca != cb {
			return false
		}
	}
	return true
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
