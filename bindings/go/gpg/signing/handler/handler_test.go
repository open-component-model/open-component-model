//go:debug fips140=on

// The tests run with GODEBUG=fips140=on, the default of OCM release builds (GOFIPS140), so that steps outside the
// Go Cryptographic Module are logged instead of silently passing. Strict mode is tested in bindings/go/gpg/fips140.

package handler

import (
	"bytes"
	"context"
	"crypto"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	_ "crypto/sha256" // registers crypto.SHA256 for makeDigest
	_ "crypto/sha512" // registers crypto.SHA384 and crypto.SHA512 for makeDigest

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler/handlertest"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler/internal/gpgbinary"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	identityv1 "ocm.software/open-component-model/bindings/go/gpg/spec/identity/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

var (
	ecdsaKeyConfig    = handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256}
	subkeyKeyConfig   = handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256, SigningSubkey: true}
	protectedKeyCfg   = handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256, Passphrase: "pw"}
	testDigestContent = []byte("component descriptor")
)

func TestGPGHandler_MissingKeys(t *testing.T) {
	r := require.New(t)
	h := handlerWithoutGPG(t)
	digest := makeDigest(t, crypto.SHA256, testDigestContent)

	_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{})
	r.ErrorIs(err, ErrMissingPrivateKey)
	_, err = h.Sign(t.Context(), digest, &v1alpha1.Config{}, nil)
	r.ErrorIs(err, ErrMissingPrivateKey)

	err = h.Verify(t.Context(), gpgSignature(digest, "irrelevant"), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{})
	r.ErrorIs(err, ErrMissingPublicKey)
}

func TestGPGHandler_InvalidInput(t *testing.T) {
	h := handlerWithoutGPG(t)
	creds := privCreds(mustGenerateKey(t, ecdsaKeyConfig))
	valid := makeDigest(t, crypto.SHA256, testDigestContent)

	tests := []struct {
		name    string
		digest  descruntime.Digest
		cfg     *v1alpha1.Config
		wantErr error
		wantMsg string
	}{
		{name: "unsupported config hash algorithm", digest: valid, cfg: &v1alpha1.Config{HashAlgorithm: "SHA521"}, wantMsg: `unsupported GPG hash algorithm "SHA521"`},
		{name: "missing digest hash algorithm", digest: descruntime.Digest{Value: valid.Value}, cfg: &v1alpha1.Config{}, wantErr: ErrMissingHashAlg},
		{name: "missing digest value", digest: descruntime.Digest{HashAlgorithm: "SHA-256"}, cfg: &v1alpha1.Config{}, wantErr: ErrMissingDigestVal},
		{name: "unsupported digest hash algorithm", digest: descruntime.Digest{HashAlgorithm: "MD5", Value: valid.Value}, cfg: &v1alpha1.Config{}, wantMsg: `unsupported hash algorithm "MD5"`},
		{name: "non-hex digest", digest: descruntime.Digest{HashAlgorithm: "SHA-256", Value: "zz"}, cfg: &v1alpha1.Config{}, wantMsg: "invalid hex digest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			_, err := h.Sign(t.Context(), tt.digest, tt.cfg, creds)
			r.Error(err)
			if tt.wantErr != nil {
				r.ErrorIs(err, tt.wantErr)
			}
			if tt.wantMsg != "" {
				r.ErrorContains(err, tt.wantMsg)
			}
		})
	}
}

func TestGPGHandler_MediaType(t *testing.T) {
	r := require.New(t)
	h := handlerWithoutGPG(t)
	signed := gpgSignature(makeDigest(t, crypto.SHA256, testDigestContent), "irrelevant")
	signed.Signature.MediaType = "application/vnd.ocm.signature.rsa"

	err := h.Verify(t.Context(), signed, &v1alpha1.Config{}, pubCreds(mustGenerateKey(t, ecdsaKeyConfig)))
	r.ErrorContains(err, `unsupported media type "application/vnd.ocm.signature.rsa"`)
}

func TestGPGHandler_CredentialIdentities(t *testing.T) {
	r := require.New(t)
	h := mustHandler(t)
	digest := makeDigest(t, crypto.SHA256, testDigestContent)

	sigIdentity, err := h.GetSigningCredentialConsumerIdentity(t.Context(), "mysig", digest, &v1alpha1.Config{})
	r.NoError(err)
	r.Equal("mysig", sigIdentity[identityv1.IdentityAttributeSignature])
	r.Equal(identityv1.V1Alpha1Type, sigIdentity.GetType())

	signed := gpgSignature(digest, "")
	signed.Name = "mysig"
	verIdentity, err := h.GetVerifyingCredentialConsumerIdentity(t.Context(), signed, &v1alpha1.Config{})
	r.NoError(err)
	r.Equal("mysig", verIdentity[identityv1.IdentityAttributeSignature])
}

func TestGPGHandler_RoundTrip(t *testing.T) {
	h := handlerWithoutGPG(t)
	tests := []struct {
		name string
		key  handlertest.KeyConfig
	}{
		{name: "v4 RSA", key: handlertest.KeyConfig{}},
		{name: "v4 ECDSA P-256", key: ecdsaKeyConfig},
		{name: "v4 ECDSA P-384", key: handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP384}},
		{name: "v4 ECDSA P-521", key: handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP521}},
		{name: "v4 EdDSA Curve25519", key: handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoEdDSA, Curve: packet.Curve25519}},
		{name: "v6 RSA", key: handlertest.KeyConfig{V6: true}},
		{name: "v6 ECDSA P-256", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256}},
		{name: "v6 Ed25519", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoEd25519}},
		{name: "v6 Ed448", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoEd448}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			key := mustGenerateKey(t, tt.key)
			digest := makeDigest(t, crypto.SHA256, testDigestContent)

			info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, privCreds(key))
			r.NoError(err)
			r.Equal(v1alpha1.AlgorithmGPG, info.Algorithm)
			r.Equal(v1alpha1.MediaTypeGPG, info.MediaType)
			r.Equal(key.Entity.PrimaryKey.KeyId, *parsedSignature(t, info.Value).IssuerKeyId)

			r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, pubCreds(key)))
		})
	}
}

// TestGPGHandler_SignatureHash checks that a configured hashAlgorithm is used exactly or rejected, and that the
// default follows the minimum of the key. A wrong minimum in signatureHash shows up as a different hash here,
// because go-crypto silently replaces hashes it considers too short.
func TestGPGHandler_SignatureHash(t *testing.T) {
	h := handlerWithoutGPG(t)
	digest := makeDigest(t, crypto.SHA256, testDigestContent)
	keys := []struct {
		name    string
		key     handlertest.KeyConfig
		minimum crypto.Hash
	}{
		{name: "RSA", key: handlertest.KeyConfig{}, minimum: crypto.SHA256},
		{name: "ECDSA P-256", key: ecdsaKeyConfig, minimum: crypto.SHA256},
		{name: "ECDSA P-384", key: handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP384}, minimum: crypto.SHA384},
		{name: "ECDSA P-521", key: handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP521}, minimum: crypto.SHA512},
		{name: "ECDSA Brainpool P-384", key: handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveBrainpoolP384}, minimum: crypto.SHA384},
		{name: "ECDSA Brainpool P-512", key: handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveBrainpoolP512}, minimum: crypto.SHA512},
		{name: "EdDSA Curve25519", key: handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoEdDSA, Curve: packet.Curve25519}, minimum: crypto.SHA256},
		{name: "v6 Ed25519", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoEd25519}, minimum: crypto.SHA256},
		{name: "v6 Ed448", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoEd448}, minimum: crypto.SHA512},
		{
			// The signing subkey's algorithm decides, not the primary key's.
			name:    "RSA primary key with ECDSA P-521 signing subkey",
			key:     handlertest.KeyConfig{SigningSubkey: true, SubkeyAlgorithm: packet.PubKeyAlgoECDSA, SubkeyCurve: packet.CurveNistP521},
			minimum: crypto.SHA512,
		},
	}
	configs := []struct {
		alg  v1alpha1.HashAlgorithm
		hash crypto.Hash
	}{
		{alg: v1alpha1.HashAlgorithmSHA256, hash: crypto.SHA256},
		{alg: v1alpha1.HashAlgorithmSHA384, hash: crypto.SHA384},
		{alg: v1alpha1.HashAlgorithmSHA512, hash: crypto.SHA512},
	}
	for _, k := range keys {
		key := mustGenerateKey(t, k.key)
		t.Run(k.name+", hashAlgorithm unset", func(t *testing.T) {
			info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, privCreds(key))
			require.NoError(t, err)
			require.Equal(t, k.minimum, parsedSignature(t, info.Value).Hash)
		})
		for _, c := range configs {
			t.Run(fmt.Sprintf("%s, hashAlgorithm %s", k.name, c.alg), func(t *testing.T) {
				r := require.New(t)
				info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{HashAlgorithm: c.alg}, privCreds(key))
				if c.hash.Size() < k.minimum.Size() {
					r.ErrorIs(err, ErrHashTooShortForKey)
					r.ErrorContains(err, "needs "+k.minimum.String()+" or longer")
					return
				}
				r.NoError(err)
				r.Equal(c.hash, parsedSignature(t, info.Value).Hash)
				r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, pubCreds(key)))
			})
		}
	}
}

func TestGPGHandler_SignKeySelection(t *testing.T) {
	h := handlerWithoutGPG(t)
	a := mustGenerateKey(t, ecdsaKeyConfig)
	b := mustGenerateKey(t, ecdsaKeyConfig)
	v6 := mustGenerateKey(t, handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256})
	sub := mustGenerateKey(t, subkeyKeyConfig)
	subkeyID := signingSubkey(sub).KeyId
	fpr := a.Fingerprint

	tests := []struct {
		name       string
		private    string
		pin        string
		wantIssuer uint64
		wantErr    error
		wantMsg    string
	}{
		{name: "no pin uses the first secret key", private: a.Private + b.Private, wantIssuer: a.Entity.PrimaryKey.KeyId},
		{name: "pin selects a later key", private: a.Private + b.Private, pin: b.Fingerprint, wantIssuer: b.Entity.PrimaryKey.KeyId},
		{name: "public keys are skipped without pin", private: a.Public + b.Private, wantIssuer: b.Entity.PrimaryKey.KeyId},
		{name: "lower-case fingerprint", private: a.Private, pin: strings.ToLower(fpr), wantIssuer: a.Entity.PrimaryKey.KeyId},
		{name: "spaced fingerprint as printed by gpg", private: a.Private, pin: spaced(fpr), wantIssuer: a.Entity.PrimaryKey.KeyId},
		{name: "0x-prefixed fingerprint", private: a.Private, pin: "0x" + fpr, wantIssuer: a.Entity.PrimaryKey.KeyId},
		{name: "v4 long key ID (fingerprint suffix)", private: a.Private, pin: fpr[24:], wantIssuer: a.Entity.PrimaryKey.KeyId},
		{name: "v6 fingerprint", private: v6.Private, pin: v6.Fingerprint, wantIssuer: v6.Entity.PrimaryKey.KeyId},
		{name: "v6 long key ID (fingerprint prefix)", private: v6.Private, pin: v6.Fingerprint[:16], wantIssuer: v6.Entity.PrimaryKey.KeyId},
		{name: "signing subkey signs without pin", private: sub.Private, wantIssuer: subkeyID},
		{name: "primary pin lets the key choose its signing subkey", private: sub.Private, pin: sub.Fingerprint, wantIssuer: subkeyID},
		{name: "subkey pin", private: sub.Private, pin: fmt.Sprintf("%X", signingSubkey(sub).Fingerprint), wantIssuer: subkeyID},
		{name: "unknown pin", private: a.Private, pin: strings.Repeat("DEADBEEF", 5), wantMsg: "no secret key matching key fingerprint"},
		{name: "pin of a public-only key", private: a.Private + b.Public, pin: b.Fingerprint, wantMsg: "no secret key matching key fingerprint"},
		{name: "public key material only", private: a.Public, wantErr: ErrNoSecretKey},
	}
	digest := makeDigest(t, crypto.SHA256, testDigestContent)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{KeyFingerprint: tt.pin},
				&gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: tt.private})
			switch {
			case tt.wantErr != nil:
				r.ErrorIs(err, tt.wantErr)
			case tt.wantMsg != "":
				r.ErrorContains(err, tt.wantMsg)
			default:
				r.NoError(err)
				r.Equal(tt.wantIssuer, *parsedSignature(t, info.Value).IssuerKeyId)
			}
		})
	}
}

func TestGPGHandler_VerifyPin(t *testing.T) {
	h := handlerWithoutGPG(t)
	a := mustGenerateKey(t, ecdsaKeyConfig)
	sub := mustGenerateKey(t, subkeyKeyConfig)
	subFpr := fmt.Sprintf("%X", signingSubkey(sub).Fingerprint)
	digest := makeDigest(t, crypto.SHA256, testDigestContent)
	bySubkey := mustSign(t, h, digest, privCreds(sub))
	byA := mustSign(t, h, digest, privCreds(a))
	keyring := &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: a.Public + sub.Public}

	tests := []struct {
		name      string
		signature string
		pin       string
		wantErr   bool
	}{
		{name: "no pin accepts any key in the keyring", signature: bySubkey},
		{name: "no pin, other key in the keyring", signature: byA},
		{name: "primary pin accepts a subkey signature", signature: bySubkey, pin: sub.Fingerprint},
		{name: "subkey pin", signature: bySubkey, pin: subFpr},
		{name: "subkey long key ID", signature: bySubkey, pin: subFpr[24:]},
		{name: "pin of another key in the keyring", signature: bySubkey, pin: a.Fingerprint, wantErr: true},
		{name: "pin of the signer's primary key for another signer", signature: byA, pin: sub.Fingerprint, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := h.Verify(t.Context(), gpgSignature(digest, tt.signature), &v1alpha1.Config{KeyFingerprint: tt.pin}, keyring)
			if tt.wantErr {
				require.ErrorContains(t, err, "does not match the configured key fingerprint")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestGPGHandler_Passphrase(t *testing.T) {
	h := handlerWithoutGPG(t)
	protected := mustGenerateKey(t, protectedKeyCfg)
	protectedSubkey := mustGenerateKey(t, handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256, SigningSubkey: true, Passphrase: "pw"})
	unprotected := mustGenerateKey(t, ecdsaKeyConfig)
	digest := makeDigest(t, crypto.SHA256, testDigestContent)

	tests := []struct {
		name       string
		key        *handlertest.Key
		passphrase string
		wantErr    error
		wantMsg    string
	}{
		{name: "correct passphrase", key: protected, passphrase: "pw"},
		{name: "correct passphrase unlocks a signing subkey", key: protectedSubkey, passphrase: "pw"},
		{name: "wrong passphrase", key: protected, passphrase: "wrong", wantMsg: "decrypt GPG private key"},
		{name: "missing passphrase", key: protected, wantErr: ErrMissingPassphrase},
		{name: "passphrase for an unprotected key is ignored", key: unprotected, passphrase: "pw"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{},
				&gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: tt.key.Private, Passphrase: tt.passphrase})
			switch {
			case tt.wantErr != nil:
				r.ErrorIs(err, tt.wantErr)
			case tt.wantMsg != "":
				r.ErrorContains(err, tt.wantMsg)
			default:
				r.NoError(err)
				r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, pubCreds(tt.key)))
			}
		})
	}
}

func TestGPGHandler_KeyMaterial(t *testing.T) {
	h := handlerWithoutGPG(t)
	key := mustGenerateKey(t, ecdsaKeyConfig)
	digest := makeDigest(t, crypto.SHA256, testDigestContent)
	signature := mustSign(t, h, digest, privCreds(key))
	binaryPrivate := entityBytes(t, func(w io.Writer) error { return key.Entity.SerializePrivateWithoutSigning(w, nil) })
	binaryPublic := entityBytes(t, key.Entity.Serialize)

	revoked := mustGenerateKey(t, ecdsaKeyConfig)
	require.NoError(t, revoked.Entity.RevokeKey(packet.KeyCompromised, "", nil))
	revocationOnly := armorBlock(t, openpgp.PublicKeyType, entityBytes(t, revoked.Entity.Revocations[0].Serialize))

	tests := []struct {
		name     string
		material string // used as private key material for sign, as public key material for verify
		verify   bool
		wantMsg  string // empty: success
	}{
		{name: "binary private key", material: string(binaryPrivate)},
		{name: "binary public key", material: string(binaryPublic), verify: true},
		{name: "private key verifies (public key derived from it)", material: key.Private, verify: true},
		{name: "public and private block in one file", material: key.Public + key.Private},
		{name: "text around the armored block", material: "my signing key:\n" + key.Private + "\n# end\n"},
		{name: "CRLF line endings", material: strings.ReplaceAll(key.Private, "\n", "\r\n")},
		{name: "signature block in key material", material: key.Public + signature, verify: true, wantMsg: `unexpected armored block "PGP SIGNATURE"`},
		{name: "standalone revocation certificate", material: key.Public + revocationOnly, verify: true, wantMsg: "parse OpenPGP key material"},
		{name: "empty armored block", material: armorBlock(t, openpgp.PublicKeyType, nil), verify: true, wantMsg: "no OpenPGP key found"},
		{name: "not OpenPGP", material: "not a key", wantMsg: "parse OpenPGP key material"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.verify {
				err = h.Verify(t.Context(), gpgSignature(digest, signature), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: tt.material})
			} else {
				_, err = h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: tt.material})
			}
			if tt.wantMsg == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantMsg)
		})
	}

	t.Run("key files", func(t *testing.T) {
		r := require.New(t)
		dir := t.TempDir()
		privFile, pubFile := dir+"/key.asc", dir+"/key.pub.asc"
		r.NoError(os.WriteFile(privFile, []byte(key.Private), 0o600))
		r.NoError(os.WriteFile(pubFile, []byte(key.Public), 0o600))
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGPFile: privFile})
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PublicKeyPGPFile: pubFile}))

		_, err = h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGPFile: dir + "/missing.asc"})
		r.ErrorContains(err, "load GPG private key")
	})
}

// TestGPGHandler_KeyValidity pins down that key validity is judged at the time of verification (see the comment at
// the VerifyDetachedSignature call): signatures made while a key was valid fail once it expires or is revoked.
func TestGPGHandler_KeyValidity(t *testing.T) {
	h := handlerWithoutGPG(t)
	digest := makeDigest(t, crypto.SHA256, testDigestContent)
	twoHoursAgo := time.Now().Add(-2 * time.Hour)

	t.Run("expired key", func(t *testing.T) {
		r := require.New(t)
		key := mustGenerateKey(t, handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256, Created: twoHoursAgo, Lifetime: time.Hour})
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, privCreds(key))
		r.ErrorContains(err, "has no valid signing key")

		madeWhileValid := signAt(t, key.Entity, digest, twoHoursAgo.Add(time.Minute))
		err = h.Verify(t.Context(), gpgSignature(digest, madeWhileValid), &v1alpha1.Config{}, pubCreds(key))
		r.ErrorIs(err, pgperrors.ErrKeyExpired)
	})

	t.Run("key that is not yet expired", func(t *testing.T) {
		key := mustGenerateKey(t, handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256, Created: twoHoursAgo, Lifetime: 24 * time.Hour})
		require.NoError(t, h.Verify(t.Context(), gpgSignature(digest, mustSign(t, h, digest, privCreds(key))), &v1alpha1.Config{}, pubCreds(key)))
	})

	for _, reason := range []packet.ReasonForRevocation{packet.KeyCompromised, packet.KeySuperseded, packet.KeyRetired} {
		t.Run(fmt.Sprintf("revoked key, reason %d", reason), func(t *testing.T) {
			r := require.New(t)
			key := mustGenerateKey(t, ecdsaKeyConfig)
			signature := mustSign(t, h, digest, privCreds(key))
			r.NoError(key.Entity.RevokeKey(reason, "", nil))

			_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: armoredPrivate(t, key.Entity)})
			r.ErrorContains(err, "has no valid signing key")
			err = h.Verify(t.Context(), gpgSignature(digest, signature), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: armoredPublic(t, key.Entity)})
			r.ErrorIs(err, pgperrors.ErrKeyRevoked)
		})
	}

	t.Run("revoked signing subkey", func(t *testing.T) {
		r := require.New(t)
		key := mustGenerateKey(t, subkeyKeyConfig)
		bySubkey := mustSign(t, h, digest, privCreds(key))
		r.Equal(signingSubkey(key).KeyId, *parsedSignature(t, bySubkey).IssuerKeyId)
		r.NoError(key.Entity.RevokeSubkey(&key.Entity.Subkeys[len(key.Entity.Subkeys)-1], packet.KeySuperseded, "", nil))
		public := &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: armoredPublic(t, key.Entity)}

		err := h.Verify(t.Context(), gpgSignature(digest, bySubkey), &v1alpha1.Config{}, public)
		r.ErrorIs(err, pgperrors.ErrKeyRevoked)

		// The primary key can sign too, so it takes over.
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: armoredPrivate(t, key.Entity)})
		r.NoError(err)
		r.Equal(key.Entity.PrimaryKey.KeyId, *parsedSignature(t, info.Value).IssuerKeyId)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, public))
	})
}

func TestGPGHandler_SignatureFormat(t *testing.T) {
	h := handlerWithoutGPG(t)
	key := mustGenerateKey(t, ecdsaKeyConfig)
	other := mustGenerateKey(t, ecdsaKeyConfig)
	digest := makeDigest(t, crypto.SHA256, testDigestContent)
	signature := mustSign(t, h, digest, privCreds(key))
	otherSignature := mustSign(t, h, digest, privCreds(other))
	body := armorBody(t, signature)
	keyring := &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: key.Public + other.Public}

	tests := []struct {
		name      string
		digest    descruntime.Digest
		signature string
		wantErr   error
		wantMsg   string
	}{
		{name: "not armored", signature: string(body), wantMsg: "not an ASCII-armored OpenPGP signature"},
		{name: "public key block instead of a signature", signature: key.Public, wantMsg: `armored "PGP PUBLIC KEY BLOCK" block`},
		{name: "two armored signatures", signature: signature + otherSignature, wantMsg: "contains 2 armored blocks"},
		{name: "two signature packets", signature: armorBlock(t, openpgp.SignatureType, append(slices.Clone(body), armorBody(t, otherSignature)...)), wantMsg: "exactly one signature packet"},
		{name: "signature followed by a key packet", signature: armorBlock(t, openpgp.SignatureType, append(slices.Clone(body), armorBody(t, key.Public)...)), wantMsg: "exactly one signature packet"},
		{name: "SHA-1 signature", signature: relabelHash(t, signature, 2), wantMsg: "unsupported hash algorithm SHA-1"},
		{name: "signer not in keyring", signature: mustSign(t, h, digest, privCreds(mustGenerateKey(t, ecdsaKeyConfig))), wantErr: pgperrors.ErrUnknownIssuer},
		{name: "other digest", digest: makeDigest(t, crypto.SHA256, []byte("tampered")), signature: signature, wantMsg: "gpg verify failed"},
		{name: "corrupted signature value", signature: corruptLastByte(t, signature), wantMsg: "gpg verify failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.digest
			if d.Value == "" {
				d = digest
			}
			err := h.Verify(t.Context(), gpgSignature(d, tt.signature), &v1alpha1.Config{}, keyring)
			require.Error(t, err)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
			if tt.wantMsg != "" {
				require.ErrorContains(t, err, tt.wantMsg)
			}
		})
	}
}

// TestGPGHandler_Fixtures verifies signatures made by the former go-crypto implementation.
func TestGPGHandler_Fixtures(t *testing.T) {
	h := handlerWithoutGPG(t)
	pub := &gpgcredentialsv1.GPGCredentials{PublicKeyPGPFile: "testdata/gocrypto/public.asc"}
	for name, hashAlg := range map[string]string{"sha256": "SHA-256", "sha512": "SHA-512"} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			sig := readFixture(t, "gocrypto/"+name+".sig.asc")
			digest := descruntime.Digest{HashAlgorithm: hashAlg, Value: readFixture(t, "gocrypto/"+name+".digest")}
			r.NoError(h.Verify(t.Context(), gpgSignature(digest, sig), &v1alpha1.Config{}, pub))
			r.Error(h.Verify(t.Context(), gpgSignature(makeDigest(t, crypto.SHA256, testDigestContent), sig), &v1alpha1.Config{}, pub))
		})
	}
}

// TestGPGHandler_GnuPGKeys uses keys and signatures made by GnuPG 2.5 (testdata/gnupg):
//   - librepgp-v5: a LibrePGP v5 Ed25519 key (the "emma.goldman" sample key of draft-ietf-openpgp-rfc4880bis, also
//     in go-crypto's test data), with a v5 signature made by gpg;
//   - pqc: GnuPG's default PQC key, a v4 Brainpool P-384 primary key with a v5 Kyber encryption subkey, which
//     go-crypto cannot parse and the handler drops;
//   - card-stub: a secret key exported from a smartcard (GNU S2K extension divert-to-card).
func TestGPGHandler_GnuPGKeys(t *testing.T) {
	h := handlerWithoutGPG(t)
	digest := descruntime.Digest{HashAlgorithm: "SHA-256", Value: readFixture(t, "gnupg/digest.hex")}
	file := func(name string) string { return "testdata/gnupg/" + name }

	for _, name := range []string{"librepgp-v5", "pqc"} {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			priv := &gpgcredentialsv1.GPGCredentials{PrivateKeyPGPFile: file(name + ".asc")}
			pub := &gpgcredentialsv1.GPGCredentials{PublicKeyPGPFile: file(name + ".pub.asc")}
			fpr := fmt.Sprintf("%X", parsedPublicKey(t, file(name+".pub.asc")).Fingerprint)

			r.NoError(h.Verify(t.Context(), gpgSignature(digest, readFixture(t, "gnupg/"+name+".sig.asc")), &v1alpha1.Config{KeyFingerprint: fpr}, pub), "signature made by gpg")
			info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{KeyFingerprint: fpr}, priv)
			r.NoError(err)
			r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{KeyFingerprint: fpr}, pub))
		})
	}

	t.Run("v5 key and signature", func(t *testing.T) {
		r := require.New(t)
		pk := parsedPublicKey(t, file("librepgp-v5.pub.asc"))
		r.Equal(5, pk.Version)
		r.Len(pk.Fingerprint, 32, "v5 fingerprints are SHA-256")
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGPFile: file("librepgp-v5.asc")})
		r.NoError(err)
		r.Equal(5, parsedSignature(t, info.Value).Version)
		r.Equal(5, parsedSignature(t, readFixture(t, "gnupg/librepgp-v5.sig.asc")).Version)
	})

	t.Run("secret key on a smartcard", func(t *testing.T) {
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGPFile: file("card-stub.asc")})
		require.ErrorContains(t, err, "the secret key is stored on a hardware token, which is not supported")
	})

	t.Run("other parse errors do not mention hardware tokens", func(t *testing.T) {
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: "not a key"})
		require.ErrorContains(t, err, "parse OpenPGP key material")
		require.NotContains(t, err.Error(), "hardware token")
	})
}

func TestGPGHandler_Keyring(t *testing.T) {
	key := mustGenerateKey(t, ecdsaKeyConfig)
	sub := mustGenerateKey(t, subkeyKeyConfig)
	subFpr := fmt.Sprintf("%X", signingSubkey(sub).Fingerprint)
	protected := mustGenerateKey(t, protectedKeyCfg)
	v6 := mustGenerateKey(t, handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256})
	digest := makeDigest(t, crypto.SHA256, testDigestContent)
	keyring := func(fpr string) *gpgcredentialsv1.GPGCredentials {
		return &gpgcredentialsv1.GPGCredentials{KeyringFingerprint: fpr}
	}

	t.Run("sign and verify run gpg only to export keys", func(t *testing.T) {
		r := require.New(t)
		h, gpg := handlerWithFakeGPG(t, key)
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, keyring(key.Fingerprint))
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, keyring(key.Fingerprint)))
		r.Equal([][]string{
			{"--version"},
			{"--batch", "--no-tty", "--pinentry-mode", "loopback", "--passphrase-fd", "0", "--armor", "--export-secret-keys", key.Fingerprint},
			{"--batch", "--no-tty", "--armor", "--export", key.Fingerprint},
		}, gpg.args())
	})

	t.Run("keyringHome selects the GnuPG home directory", func(t *testing.T) {
		r := require.New(t)
		h, gpg := handlerWithFakeGPG(t, key)
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{KeyringFingerprint: key.Fingerprint, KeyringHome: "/h"})
		r.NoError(err)
		r.Equal([]string{"--batch", "--no-tty", "--homedir", "/h"}, gpg.args()[1][:4])
	})

	t.Run("fingerprint is normalized before it reaches gpg", func(t *testing.T) {
		r := require.New(t)
		h, gpg := handlerWithFakeGPG(t, key)
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, keyring("0x"+spaced(key.Fingerprint)))
		r.NoError(err)
		r.Equal(key.Fingerprint, gpg.args()[1][len(gpg.args()[1])-1])
	})

	t.Run("v6 fingerprint", func(t *testing.T) {
		r := require.New(t)
		h, _ := handlerWithFakeGPG(t, v6)
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, keyring(v6.Fingerprint))
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, keyring(v6.Fingerprint)))
	})

	t.Run("passphrase goes to gpg on stdin and unlocks the key in OCM", func(t *testing.T) {
		r := require.New(t)
		h, gpg := handlerWithFakeGPG(t, protected)
		creds := &gpgcredentialsv1.GPGCredentials{KeyringFingerprint: protected.Fingerprint, Passphrase: "pw"}
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, creds)
		r.NoError(err)
		r.Equal("pw", string(gpg.calls[1].stdin))
		r.NotContains(strings.Join(gpg.args()[1], " "), "pw")
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, creds))
	})

	t.Run("protected key without passphrase", func(t *testing.T) {
		h, _ := handlerWithFakeGPG(t, protected)
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, keyring(protected.Fingerprint))
		require.ErrorIs(t, err, ErrMissingPassphrase)
	})

	t.Run("subkey fingerprint signs with that subkey and pins verification to it", func(t *testing.T) {
		r := require.New(t)
		h, _ := handlerWithFakeGPG(t, sub)
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, keyring(subFpr))
		r.NoError(err)
		r.Equal(signingSubkey(sub).KeyId, *parsedSignature(t, info.Value).IssuerKeyId)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, keyring(subFpr)))
	})

	t.Run("config keyFingerprint takes precedence over keyringFingerprint", func(t *testing.T) {
		r := require.New(t)
		h, _ := handlerWithFakeGPG(t, sub)
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{KeyFingerprint: subFpr}, keyring(sub.Fingerprint))
		r.NoError(err)
		r.Equal(signingSubkey(sub).KeyId, *parsedSignature(t, info.Value).IssuerKeyId)
		err = h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{KeyFingerprint: key.Fingerprint}, keyring(sub.Fingerprint))
		r.ErrorContains(err, "does not match the configured key fingerprint")
	})

	t.Run("key material next to keyringFingerprint", func(t *testing.T) {
		h, gpg := handlerWithFakeGPG(t, key)
		for _, creds := range []gpgcredentialsv1.GPGCredentials{
			{KeyringFingerprint: key.Fingerprint, PrivateKeyPGP: "x"},
			{KeyringFingerprint: key.Fingerprint, PrivateKeyPGPFile: "x"},
			{KeyringFingerprint: key.Fingerprint, PublicKeyPGP: "x"},
			{KeyringFingerprint: key.Fingerprint, PublicKeyPGPFile: "x"},
		} {
			_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &creds)
			require.ErrorIs(t, err, ErrKeyMaterialWithKeyring)
			err = h.Verify(t.Context(), gpgSignature(digest, "irrelevant"), &v1alpha1.Config{}, &creds)
			require.ErrorIs(t, err, ErrKeyMaterialWithKeyring)
		}
		require.Empty(t, gpg.calls)
	})

	for name, fpr := range map[string]string{
		"long key ID":         key.Fingerprint[24:],
		"truncated":           key.Fingerprint[:39],
		"not hex":             strings.Repeat("Z", 40),
		"between v4 and v6":   key.Fingerprint + "0000",
		"longer than v6 size": v6.Fingerprint + "00",
	} {
		t.Run("invalid keyringFingerprint: "+name, func(t *testing.T) {
			h, gpg := handlerWithFakeGPG(t, key)
			_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, keyring(fpr))
			require.ErrorIs(t, err, ErrKeyringRequiresFingerprint)
			err = h.Verify(t.Context(), gpgSignature(digest, "irrelevant"), &v1alpha1.Config{}, keyring(fpr))
			require.ErrorIs(t, err, ErrKeyringRequiresFingerprint)
			require.Empty(t, gpg.calls)
		})
	}

	t.Run("key not in the keyring", func(t *testing.T) {
		r := require.New(t)
		h, _ := handlerWithFakeGPG(t, nil) // gpg exports nothing and exits 0 for unknown keys
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, keyring(key.Fingerprint))
		r.ErrorContains(err, fmt.Sprintf("no secret key %s found in the GnuPG keyring", key.Fingerprint))
		err = h.Verify(t.Context(), gpgSignature(digest, "irrelevant"), &v1alpha1.Config{}, keyring(key.Fingerprint))
		r.ErrorContains(err, fmt.Sprintf("key %s not found in the GnuPG keyring", key.Fingerprint))
	})

	t.Run("gpg failure", func(t *testing.T) {
		r := require.New(t)
		h, gpg := handlerWithFakeGPG(t, key)
		gpg.fail = errors.New("exit status 2")
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, keyring(key.Fingerprint))
		r.ErrorContains(err, "export GPG private key from the GnuPG keyring: gpg export-secret-keys failed: exit status 2")
		r.ErrorContains(err, "gpg: agent refused")
	})

	t.Run("no gpg on PATH: keyring fails, key material does not need gpg", func(t *testing.T) {
		r := require.New(t)
		h := handlerWithoutGPG(t)
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, keyring(key.Fingerprint))
		r.ErrorIs(err, ErrGPGNotFound)
		err = h.Verify(t.Context(), gpgSignature(digest, "irrelevant"), &v1alpha1.Config{}, keyring(key.Fingerprint))
		r.ErrorIs(err, ErrGPGNotFound)

		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, privCreds(key))
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, pubCreds(key)))
	})
}

// TestGPGHandler_FIPSBoundaryLogs checks the fips140=on rule of ADR 0030: steps outside the Go Cryptographic Module
// run, but are logged at debug level.
func TestGPGHandler_FIPSBoundaryLogs(t *testing.T) {
	logs := captureDebugLogs(t)
	digest := makeDigest(t, crypto.SHA256, testDigestContent)
	const suffix = " runs outside the FIPS 140-3 boundary (GODEBUG=fips140=only rejects it)"

	tests := []struct {
		name       string
		key        handlertest.KeyConfig
		passphrase string
		keyring    bool
		wantLogs   []string
	}{
		{name: "v6 RSA", key: handlertest.KeyConfig{V6: true}},
		{name: "v6 ECDSA P-384", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP384}},
		{name: "v4 key", key: handlertest.KeyConfig{}, wantLogs: []string{"OpenPGP key with a SHA-1 fingerprint"}},
		{name: "v6 Ed25519", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoEd25519}, wantLogs: []string{"GPG key algorithm Ed25519"}},
		{name: "v6 ECDSA brainpool", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveBrainpoolP256}, wantLogs: []string{"GPG key algorithm ECDSA BrainpoolP256"}},
		{
			name:     "v6 Ed25519 primary key with RSA signing subkey",
			key:      handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoEd25519, SigningSubkey: true, SubkeyAlgorithm: packet.PubKeyAlgoRSA},
			wantLogs: []string{"GPG key algorithm Ed25519"},
		},
		{name: "v6 protected key", key: handlertest.KeyConfig{V6: true, Passphrase: "pw"}, passphrase: "pw", wantLogs: []string{"unlocking a passphrase-protected GPG key"}},
		{name: "v6 key from the keyring", key: handlertest.KeyConfig{V6: true}, keyring: true, wantLogs: []string{"exporting a secret key from the GnuPG keyring"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			key := mustGenerateKey(t, tt.key)
			h, _ := handlerWithFakeGPG(t, key)
			creds := &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: key.Private, Passphrase: tt.passphrase}
			if tt.keyring {
				creds = &gpgcredentialsv1.GPGCredentials{KeyringFingerprint: key.Fingerprint}
			}
			logs.take()
			_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, creds)
			r.NoError(err)

			var got []string
			for _, msg := range logs.take() {
				if boundary, ok := strings.CutSuffix(msg, suffix); ok {
					got = append(got, boundary)
				}
			}
			r.Equal(tt.wantLogs, got)
		})
	}
}

func TestGPGHandler_Concurrent(t *testing.T) {
	h := handlerWithoutGPG(t)
	key := mustGenerateKey(t, protectedKeyCfg)
	creds := &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: key.Private, Passphrase: "pw"}

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 16 {
		digest := makeDigest(t, crypto.SHA256, fmt.Appendf(nil, "descriptor %d", i))
		wg.Go(func() {
			info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, creds)
			if err == nil {
				err = h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, creds)
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

// ---- helpers ----

func mustHandler(t *testing.T) *Handler {
	t.Helper()
	h, err := New(v1alpha1.Scheme)
	require.NoError(t, err)
	return h
}

// handlerWithoutGPG returns a handler whose gpg lookup always fails.
func handlerWithoutGPG(t *testing.T) *Handler {
	t.Helper()
	h := mustHandler(t)
	h.gpgBinary = gpgbinary.New(gpgbinary.WithLookPath(func(string) (string, error) { return "", exec.ErrNotFound }))
	return h
}

type gpgCall struct {
	args  []string
	stdin []byte
}

// fakeGPG answers --version, and --export / --export-secret-keys with the keys of key (nothing if key is nil).
type fakeGPG struct {
	key   *handlertest.Key
	fail  error
	calls []gpgCall
}

func (f *fakeGPG) exec(_ context.Context, _ string, args []string, stdin []byte) ([]byte, []byte, error) {
	f.calls = append(f.calls, gpgCall{args: args, stdin: stdin})
	switch {
	case slices.Equal(args, []string{"--version"}):
		return []byte("gpg (GnuPG) 2.4.4\nlibgcrypt 1.10.3\n"), nil, nil
	case f.fail != nil:
		return nil, []byte("gpg: agent refused"), f.fail
	case f.key == nil:
		return nil, nil, nil
	case slices.Contains(args, "--export-secret-keys"):
		return []byte(f.key.Private), nil, nil
	case slices.Contains(args, "--export"):
		return []byte(f.key.Public), nil, nil
	}
	return nil, nil, fmt.Errorf("unexpected gpg invocation: %v", args)
}

func (f *fakeGPG) args() [][]string {
	var args [][]string
	for _, c := range f.calls {
		args = append(args, c.args)
	}
	return args
}

func handlerWithFakeGPG(t *testing.T, key *handlertest.Key) (*Handler, *fakeGPG) {
	t.Helper()
	h := mustHandler(t)
	gpg := &fakeGPG{key: key}
	h.gpgBinary = gpgbinary.New(
		gpgbinary.WithLookPath(func(file string) (string, error) { return "/fake/bin/" + file, nil }),
		gpgbinary.WithExec(gpg.exec),
	)
	return h, gpg
}

// captureDebugLogs routes the default logger, which the handler logs to, into the returned recorder for this test.
func captureDebugLogs(t *testing.T) *logRecorder {
	t.Helper()
	rec := &logRecorder{}
	prev := slog.Default()
	slog.SetDefault(slog.New(rec))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return rec
}

// logRecorder is a slog.Handler that records the messages of all records.
type logRecorder struct {
	mu       sync.Mutex
	messages []string
}

func (*logRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (l *logRecorder) WithAttrs([]slog.Attr) slog.Handler     { return l }
func (l *logRecorder) WithGroup(string) slog.Handler          { return l }

func (l *logRecorder) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, r.Message)
	return nil
}

// take returns and clears the recorded messages.
func (l *logRecorder) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	m := l.messages
	l.messages = nil
	return m
}

func mustGenerateKey(t *testing.T, cfg handlertest.KeyConfig) *handlertest.Key {
	t.Helper()
	key, err := handlertest.GenerateKey(cfg)
	require.NoError(t, err)
	return key
}

func mustSign(t *testing.T, h *Handler, digest descruntime.Digest, creds *gpgcredentialsv1.GPGCredentials) string {
	t.Helper()
	info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, creds)
	require.NoError(t, err)
	return info.Value
}

// signAt makes a signature with the creation time at, bypassing the handler, which always signs now.
func signAt(t *testing.T, e *openpgp.Entity, digest descruntime.Digest, at time.Time) string {
	t.Helper()
	data, err := hex.DecodeString(digest.Value)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, openpgp.ArmoredDetachSign(&buf, e, bytes.NewReader(data), &packet.Config{
		DefaultHash: crypto.SHA256,
		Time:        func() time.Time { return at },
	}))
	return buf.String()
}

// signingSubkey returns the signing subkey added by KeyConfig.SigningSubkey (NewEntity adds an encryption subkey first).
func signingSubkey(key *handlertest.Key) *packet.PublicKey {
	return key.Entity.Subkeys[len(key.Entity.Subkeys)-1].PublicKey
}

func parsedSignature(t *testing.T, value string) *packet.Signature {
	t.Helper()
	sig, _, err := parseSignature(value)
	require.NoError(t, err)
	return sig
}

func privCreds(key *handlertest.Key) *gpgcredentialsv1.GPGCredentials {
	return &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: key.Private}
}

func pubCreds(key *handlertest.Key) *gpgcredentialsv1.GPGCredentials {
	return &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: key.Public}
}

func armoredPublic(t *testing.T, e *openpgp.Entity) string {
	t.Helper()
	s, err := handlertest.ArmoredPublicKey(e)
	require.NoError(t, err)
	return s
}

func armoredPrivate(t *testing.T, e *openpgp.Entity) string {
	t.Helper()
	return armorBlock(t, openpgp.PrivateKeyType, entityBytes(t, func(w io.Writer) error { return e.SerializePrivateWithoutSigning(w, nil) }))
}

func entityBytes(t *testing.T, serialize func(io.Writer) error) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, serialize(&buf))
	return buf.Bytes()
}

func armorBlock(t *testing.T, blockType string, data []byte) string {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, blockType, nil)
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.String()
}

func armorBody(t *testing.T, armored string) []byte {
	t.Helper()
	block, err := armor.Decode(strings.NewReader(armored))
	require.NoError(t, err)
	body, err := io.ReadAll(block.Body)
	require.NoError(t, err)
	return body
}

// relabelHash changes the hash algorithm ID of a signature, the fourth byte of a v4 or v6 signature packet.
func relabelHash(t *testing.T, signature string, hashID uint8) string {
	t.Helper()
	op, err := packet.NewOpaqueReader(bytes.NewReader(armorBody(t, signature))).Next()
	require.NoError(t, err)
	op.Contents[3] = hashID
	var buf bytes.Buffer
	require.NoError(t, op.Serialize(&buf))
	return armorBlock(t, openpgp.SignatureType, buf.Bytes())
}

func corruptLastByte(t *testing.T, signature string) string {
	t.Helper()
	body := armorBody(t, signature)
	body[len(body)-1] ^= 0xFF
	return armorBlock(t, openpgp.SignatureType, body)
}

func spaced(fpr string) string {
	var parts []string
	for i := 0; i < len(fpr); i += 4 {
		parts = append(parts, fpr[i:min(i+4, len(fpr))])
	}
	return strings.Join(parts, " ")
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

func gpgSignature(digest descruntime.Digest, value string) descruntime.Signature {
	return descruntime.Signature{
		Name:   "test",
		Digest: digest,
		Signature: descruntime.SignatureInfo{
			Algorithm: v1alpha1.AlgorithmGPG,
			MediaType: v1alpha1.MediaTypeGPG,
			Value:     value,
		},
	}
}

// readFixture returns the trimmed content of a file under testdata.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)
	return strings.TrimSpace(string(b))
}

// parsedPublicKey returns the primary key of the key file at path, read like the handler reads key material.
func parsedPublicKey(t *testing.T, path string) *packet.PublicKey {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	el, err := readKeyRing(t.Context(), b)
	require.NoError(t, err)
	require.Len(t, el, 1)
	return el[0].PrimaryKey
}
