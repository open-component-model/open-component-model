// Package handler implements OpenPGP (GPG) signing and verification for OCM.
// It supports passphrase-protected private keys via the credential map.
// Signatures are stored as ASCII-armored OpenPGP detached signatures.
package handler

import (
	"bytes"
	"context"
	"crypto"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ProtonMail/go-crypto/openpgp"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	gpgcredentials "ocm.software/open-component-model/bindings/go/gpg/signing/handler/internal/credentials"
	"ocm.software/open-component-model/bindings/go/gpg/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Identity attribute keys used for credential consumer identities.
const (
	IdentityAttributeSignature = "signature"
)

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
	_ runtime.Typed,
	creds map[string]string,
) (descruntime.SignatureInfo, error) {
	entity, err := gpgcredentials.PrivateEntityFromCredentials(creds)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("load GPG private key: %w", err)
	}
	if entity == nil {
		return descruntime.SignatureInfo{}, ErrMissingPrivateKey
	}

	_, digestBytes, err := parseDigest(unsigned)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}

	var sigBuf bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&sigBuf, entity, bytes.NewReader(digestBytes), nil); err != nil {
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
	_ runtime.Typed,
	creds map[string]string,
) error {
	if signed.Signature.MediaType != v1alpha1.MediaTypeGPG {
		return fmt.Errorf("unsupported media type %q for GPG verification", signed.Signature.MediaType)
	}

	keyring, err := gpgcredentials.PublicKeyRingFromCredentials(creds)
	if err != nil {
		return fmt.Errorf("load GPG public key: %w", err)
	}
	if len(keyring) == 0 {
		return ErrMissingPublicKey
	}

	_, digestBytes, err := parseDigest(signed.Digest)
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
	id[IdentityAttributeSignature] = name
	return id, nil
}

// GetVerifyingCredentialConsumerIdentity returns the credential consumer identity for verification.
func (*Handler) GetVerifyingCredentialConsumerIdentity(
	_ context.Context,
	signed descruntime.Signature,
	_ runtime.Typed,
) (runtime.Identity, error) {
	id := baseIdentity()
	id[IdentityAttributeSignature] = signed.Name
	return id, nil
}

func baseIdentity() runtime.Identity {
	id := runtime.Identity{}
	id.SetType(gpgcredentials.IdentityTypeGPG)
	return id
}

// parseDigest extracts the hash function and raw digest bytes from a descriptor digest.
// For GPG signing we sign the raw digest bytes directly (the hex-decoded hash value).
func parseDigest(d descruntime.Digest) (crypto.Hash, []byte, error) {
	if d.HashAlgorithm == "" {
		return 0, nil, ErrMissingHashAlg
	}
	if d.Value == "" {
		return 0, nil, ErrMissingDigestVal
	}
	b, err := hex.DecodeString(d.Value)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid hex digest: %w", err)
	}
	h, err := hashFromString(d.HashAlgorithm)
	if err != nil {
		return 0, nil, err
	}
	return h, b, nil
}

func hashFromString(alg string) (crypto.Hash, error) {
	switch alg {
	case crypto.SHA256.String():
		return crypto.SHA256, nil
	case crypto.SHA384.String():
		return crypto.SHA384, nil
	case crypto.SHA512.String():
		return crypto.SHA512, nil
	}
	return 0, fmt.Errorf("unsupported hash algorithm %q", alg)
}
