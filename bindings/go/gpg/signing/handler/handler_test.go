package handler

import (
	"bytes"
	"context"
	"crypto"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	identityv1 "ocm.software/open-component-model/bindings/go/gpg/spec/identity/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

func TestGPGHandler_RoundTrip_Unprotected(t *testing.T) {
	h := mustHandler(t)
	entity := mustEntity(t, "")

	privCreds := armoredPrivKey(t, entity)
	pubCreds := armoredPubKey(t, entity)

	digest := makeDigest(t, crypto.SHA256, []byte("hello world"))

	sig, err := h.Sign(context.Background(), digest, &v1alpha1.Config{}, privCreds)
	require.NoError(t, err)
	require.Equal(t, v1alpha1.AlgorithmGPG, sig.Algorithm)
	require.Equal(t, v1alpha1.MediaTypeGPG, sig.MediaType)
	require.NotEmpty(t, sig.Value)

	signed := descruntime.Signature{
		Name:      "test",
		Digest:    digest,
		Signature: sig,
	}

	err = h.Verify(context.Background(), signed, &v1alpha1.Config{}, pubCreds)
	require.NoError(t, err)
}

func TestGPGHandler_RoundTrip_PassphraseProtected(t *testing.T) {
	const passphrase = "test-passphrase-for-unit-test"

	h := mustHandler(t)
	entity := mustEntity(t, passphrase)

	privCreds := armoredPrivKeyWithPassphrase(t, entity, passphrase)
	pubCreds := armoredPubKey(t, entity)

	digest := makeDigest(t, crypto.SHA256, []byte("hello world"))

	sig, err := h.Sign(context.Background(), digest, &v1alpha1.Config{}, privCreds)
	require.NoError(t, err)

	signed := descruntime.Signature{
		Name:      "test",
		Digest:    digest,
		Signature: sig,
	}

	err = h.Verify(context.Background(), signed, &v1alpha1.Config{}, pubCreds)
	require.NoError(t, err)
}

func TestGPGHandler_WrongPassphrase(t *testing.T) {
	const passphrase = "test-correct-passphrase"

	h := mustHandler(t)
	entity := mustEntity(t, passphrase)

	privCreds := armoredPrivKeyWithPassphrase(t, entity, "test-wrong-passphrase")

	digest := makeDigest(t, crypto.SHA256, []byte("hello world"))

	_, err := h.Sign(context.Background(), digest, &v1alpha1.Config{}, privCreds)
	require.Error(t, err)
}

func TestGPGHandler_WrongPublicKey(t *testing.T) {
	h := mustHandler(t)
	signingEntity := mustEntity(t, "")
	otherEntity := mustEntity(t, "")

	privCreds := armoredPrivKey(t, signingEntity)
	wrongPubCreds := armoredPubKey(t, otherEntity)

	digest := makeDigest(t, crypto.SHA256, []byte("hello world"))

	sig, err := h.Sign(context.Background(), digest, &v1alpha1.Config{}, privCreds)
	require.NoError(t, err)

	signed := descruntime.Signature{
		Name:      "test",
		Digest:    digest,
		Signature: sig,
	}

	err = h.Verify(context.Background(), signed, &v1alpha1.Config{}, wrongPubCreds)
	require.Error(t, err)
}

func TestGPGHandler_MissingPrivateKey(t *testing.T) {
	h := mustHandler(t)
	digest := makeDigest(t, crypto.SHA256, []byte("hello world"))

	_, err := h.Sign(context.Background(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{})
	require.ErrorIs(t, err, ErrMissingPrivateKey)
}

func TestGPGHandler_MissingPublicKey(t *testing.T) {
	h := mustHandler(t)
	entity := mustEntity(t, "")
	privCreds := armoredPrivKey(t, entity)
	digest := makeDigest(t, crypto.SHA256, []byte("hello world"))

	sig, err := h.Sign(context.Background(), digest, &v1alpha1.Config{}, privCreds)
	require.NoError(t, err)

	signed := descruntime.Signature{
		Name:      "test",
		Digest:    digest,
		Signature: sig,
	}

	err = h.Verify(context.Background(), signed, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{})
	require.ErrorIs(t, err, ErrMissingPublicKey)
}

func TestGPGHandler_CredentialIdentities(t *testing.T) {
	h := mustHandler(t)
	digest := makeDigest(t, crypto.SHA256, []byte("data"))

	sigIdentity, err := h.GetSigningCredentialConsumerIdentity(context.Background(), "mysig", digest, &v1alpha1.Config{})
	require.NoError(t, err)
	require.Equal(t, "mysig", sigIdentity[identityv1.IdentityAttributeSignature])
	require.Equal(t, identityv1.V1Alpha1Type, sigIdentity.GetType())

	signed := descruntime.Signature{
		Name:   "mysig",
		Digest: digest,
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmGPG,
			MediaType: v1alpha1.MediaTypeGPG,
		},
	}
	verIdentity, err := h.GetVerifyingCredentialConsumerIdentity(context.Background(), signed, &v1alpha1.Config{})
	require.NoError(t, err)
	require.Equal(t, "mysig", verIdentity[identityv1.IdentityAttributeSignature])
}

func TestGPGHandler_HashAlgorithm_SHA512(t *testing.T) {
	h := mustHandler(t)
	entity := mustEntity(t, "")

	privCreds := armoredPrivKey(t, entity)
	pubCreds := armoredPubKey(t, entity)

	digest := makeDigest(t, crypto.SHA512, []byte("hello world"))
	cfg := &v1alpha1.Config{HashAlgorithm: v1alpha1.HashAlgorithmSHA512}

	sig, err := h.Sign(context.Background(), digest, cfg, privCreds)
	require.NoError(t, err)

	signed := descruntime.Signature{Name: "test", Digest: digest, Signature: sig}
	require.NoError(t, h.Verify(context.Background(), signed, cfg, pubCreds))
}

func TestGPGHandler_KeyFingerprint_Match(t *testing.T) {
	h := mustHandler(t)
	entity := mustEntity(t, "")
	fp := fmt.Sprintf("%X", entity.PrimaryKey.Fingerprint)

	privCreds := armoredPrivKey(t, entity)
	pubCreds := armoredPubKey(t, entity)
	digest := makeDigest(t, crypto.SHA256, []byte("fingerprint test"))
	cfg := &v1alpha1.Config{KeyFingerprint: fp}

	sig, err := h.Sign(context.Background(), digest, cfg, privCreds)
	require.NoError(t, err)

	signed := descruntime.Signature{Name: "test", Digest: digest, Signature: sig}
	require.NoError(t, h.Verify(context.Background(), signed, cfg, pubCreds))
}

func TestGPGHandler_KeyFingerprint_NoMatch(t *testing.T) {
	h := mustHandler(t)
	entity := mustEntity(t, "")

	privCreds := armoredPrivKey(t, entity)
	digest := makeDigest(t, crypto.SHA256, []byte("fingerprint test"))
	cfg := &v1alpha1.Config{KeyFingerprint: "DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF"}

	_, err := h.Sign(context.Background(), digest, cfg, privCreds)
	require.Error(t, err)
}

func TestGPGHandler_InvalidHashAlgorithm(t *testing.T) {
	h := mustHandler(t)
	entity := mustEntity(t, "")

	privCreds := armoredPrivKey(t, entity)
	digest := makeDigest(t, crypto.SHA256, []byte("hash alg test"))
	cfg := &v1alpha1.Config{HashAlgorithm: "SHA521"}

	_, err := h.Sign(context.Background(), digest, cfg, privCreds)
	require.Error(t, err)
	require.ErrorContains(t, err, "SHA521")
}

// ---- helpers ----

func mustHandler(t *testing.T) *Handler {
	t.Helper()
	h, err := New(v1alpha1.Scheme)
	require.NoError(t, err)
	return h
}

func mustEntity(t *testing.T, passphrase string) *openpgp.Entity {
	t.Helper()
	cfg := &packet.Config{RSABits: 2048}
	entity, err := openpgp.NewEntity("test", "", "test@example.com", cfg)
	require.NoError(t, err)
	if passphrase != "" {
		require.NoError(t, entity.EncryptPrivateKeys([]byte(passphrase), cfg))
	}
	return entity
}

func makeDigest(t *testing.T, h crypto.Hash, data []byte) descruntime.Digest {
	t.Helper()
	sum := h.New()
	_, err := sum.Write(data)
	require.NoError(t, err)
	return descruntime.Digest{
		HashAlgorithm:          h.String(),
		NormalisationAlgorithm: "jsonNormalisation/v4alpha1",
		Value:                  hex.EncodeToString(sum.Sum(nil)),
	}
}

func armoredPrivKey(t *testing.T, entity *openpgp.Entity) *gpgcredentialsv1.GPGCredentials {
	t.Helper()
	return armoredPrivKeyWithPassphrase(t, entity, "")
}

func armoredPrivKeyWithPassphrase(t *testing.T, entity *openpgp.Entity, passphrase string) *gpgcredentialsv1.GPGCredentials {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PrivateKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.SerializePrivateWithoutSigning(w, nil))
	require.NoError(t, w.Close())
	return &gpgcredentialsv1.GPGCredentials{
		PrivateKeyPGP: buf.String(),
		Passphrase:    passphrase,
	}
}

func armoredPubKey(t *testing.T, entity *openpgp.Entity) *gpgcredentialsv1.GPGCredentials {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.Serialize(w))
	require.NoError(t, w.Close())
	return &gpgcredentialsv1.GPGCredentials{
		PublicKeyPGP: buf.String(),
	}
}
