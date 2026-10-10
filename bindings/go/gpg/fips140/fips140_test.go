// Package fips140 runs GPG signing and verification with GODEBUG=fips140=only.
//
// In strict mode the GPG signing handler accepts only unprotected v6 keys whose signing-capable keys all use RSA or
// ECDSA on a NIST curve, and SHA-2 signatures; it rejects everything else with an error instead of panicking. The
// mode is fixed per process, so the directive below applies to this package's test binary only. Fixtures are made
// inside fips140.WithoutEnforcement: key generation is setup, not code under test.
//
//go:debug fips140=only
package fips140

import (
	"bytes"
	"crypto"
	"crypto/fips140"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler/handlertest"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

func TestStrictMode_RoundTrip(t *testing.T) {
	requireStrict(t)
	tests := []struct {
		name string
		key  handlertest.KeyConfig
		hash v1alpha1.HashAlgorithm
	}{
		{name: "v6 RSA", key: handlertest.KeyConfig{V6: true}},
		{name: "v6 RSA SHA-384", key: handlertest.KeyConfig{V6: true}, hash: v1alpha1.HashAlgorithmSHA384},
		{name: "v6 RSA SHA-512", key: handlertest.KeyConfig{V6: true}, hash: v1alpha1.HashAlgorithmSHA512},
		{name: "v6 ECDSA P-256", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256}},
		{name: "v6 ECDSA P-384", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP384}},
		{name: "v6 ECDSA P-521", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP521}},
		{
			name: "v6 RSA with ECDSA P-256 signing subkey",
			key:  handlertest.KeyConfig{V6: true, SigningSubkey: true, SubkeyAlgorithm: packet.PubKeyAlgoECDSA, SubkeyCurve: packet.CurveNistP256},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			key := generateKey(t, tt.key)
			cfg := &v1alpha1.Config{HashAlgorithm: tt.hash}
			sig, err := mustHandler(t).Sign(t.Context(), testDigest(), cfg, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: key.Private})
			r.NoError(err)
			r.NoError(mustHandler(t).Verify(t.Context(), signed(sig), cfg, &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: key.Public}))
		})
	}
}

func TestStrictMode_Rejected(t *testing.T) {
	requireStrict(t)
	tests := []struct {
		name    string
		key     handlertest.KeyConfig
		wantErr error
	}{
		{name: "v4 RSA", key: handlertest.KeyConfig{}, wantErr: handler.ErrV4KeyInFIPSMode},
		{name: "v4 ECDSA P-256", key: handlertest.KeyConfig{Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveNistP256}, wantErr: handler.ErrV4KeyInFIPSMode},
		{name: "v6 Ed25519", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoEd25519}, wantErr: handler.ErrAlgorithmInFIPSMode},
		{name: "v6 Ed448", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoEd448}, wantErr: handler.ErrAlgorithmInFIPSMode},
		{name: "v6 ECDSA brainpool P-256", key: handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoECDSA, Curve: packet.CurveBrainpoolP256}, wantErr: handler.ErrAlgorithmInFIPSMode},
		{
			// go-crypto verifies the primary key's self-signatures while parsing, so an Ed25519 primary key
			// leaves the module even if an approved subkey signs.
			name:    "v6 Ed25519 primary key with RSA signing subkey",
			key:     handlertest.KeyConfig{V6: true, Algorithm: packet.PubKeyAlgoEd25519, SigningSubkey: true, SubkeyAlgorithm: packet.PubKeyAlgoRSA},
			wantErr: handler.ErrAlgorithmInFIPSMode,
		},
		{
			name:    "v6 RSA primary key with Ed25519 signing subkey",
			key:     handlertest.KeyConfig{V6: true, SigningSubkey: true, SubkeyAlgorithm: packet.PubKeyAlgoEd25519},
			wantErr: handler.ErrAlgorithmInFIPSMode,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := generateKey(t, tt.key)
			var sig descruntime.SignatureInfo
			fips140.WithoutEnforcement(func() {
				var err error
				sig, err = mustHandler(t).Sign(t.Context(), testDigest(), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: key.Private})
				require.NoError(t, err)
			})

			t.Run("sign", func(t *testing.T) {
				_, err := mustHandler(t).Sign(t.Context(), testDigest(), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: key.Private})
				require.ErrorIs(t, err, tt.wantErr)
			})
			t.Run("verify", func(t *testing.T) {
				err := mustHandler(t).Verify(t.Context(), signed(sig), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: key.Public})
				require.ErrorIs(t, err, tt.wantErr)
			})
		})
	}
}

func TestStrictMode_ProtectedKeyRejected(t *testing.T) {
	requireStrict(t)
	key := generateKey(t, handlertest.KeyConfig{V6: true, Passphrase: "pw"})
	_, err := mustHandler(t).Sign(t.Context(), testDigest(), &v1alpha1.Config{},
		&gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: key.Private, Passphrase: "pw"})
	require.ErrorIs(t, err, handler.ErrProtectedKeyInFIPSMode)
}

func TestStrictMode_SHA1SignatureRejected(t *testing.T) {
	requireStrict(t)
	r := require.New(t)
	key := generateKey(t, handlertest.KeyConfig{V6: true})
	sig, err := mustHandler(t).Sign(t.Context(), testDigest(), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: key.Private})
	r.NoError(err)

	// crypto/sha1 is unavailable here, so the SHA-256 signature is relabeled as SHA-1: the hash algorithm is
	// the fourth byte of a v4 or v6 signature packet. Verify must reject it before hashing.
	block, err := armor.Decode(strings.NewReader(sig.Value))
	r.NoError(err)
	body, err := io.ReadAll(block.Body)
	r.NoError(err)
	op, err := packet.NewOpaqueReader(bytes.NewReader(body)).Next()
	r.NoError(err)
	r.Equal(uint8(8), op.Contents[3], "SHA-256")
	op.Contents[3] = 2 // SHA-1
	var relabeled, armored bytes.Buffer
	r.NoError(op.Serialize(&relabeled))
	w, err := armor.Encode(&armored, openpgp.SignatureType, nil)
	r.NoError(err)
	_, err = w.Write(relabeled.Bytes())
	r.NoError(err)
	r.NoError(w.Close())
	sig.Value = armored.String()

	err = mustHandler(t).Verify(t.Context(), signed(sig), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: key.Public})
	// go-crypto already rejects the packet while parsing, because crypto/sha1 is unavailable in strict mode.
	r.ErrorContains(err, "parse GPG signature")
}

func TestStrictMode_KeyringSecretKeyRejected(t *testing.T) {
	requireStrict(t)
	// The FIPS check precedes the gpg lookup, so the result does not depend on gpg being installed.
	_, err := mustHandler(t).Sign(t.Context(), testDigest(), &v1alpha1.Config{},
		&gpgcredentialsv1.GPGCredentials{KeyringFingerprint: strings.Repeat("A", 40)})
	require.ErrorIs(t, err, handler.ErrKeyringSecretKeyInFIPSMode)
}

// TestStrictMode_GnuPGKeys uses the GnuPG fixtures of the handler tests. v5 keys have SHA-256 fingerprints, so the
// v5 Ed25519 key is rejected for its algorithm, not its version; the PQC key's primary key is v4.
func TestStrictMode_GnuPGKeys(t *testing.T) {
	requireStrict(t)
	tests := []struct {
		key     string
		wantErr error
	}{
		{key: "librepgp-v5", wantErr: handler.ErrAlgorithmInFIPSMode},
		{key: "pqc", wantErr: handler.ErrV4KeyInFIPSMode},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			_, err := mustHandler(t).Sign(t.Context(), testDigest(), &v1alpha1.Config{},
				&gpgcredentialsv1.GPGCredentials{PrivateKeyPGPFile: "../signing/handler/testdata/gnupg/" + tt.key + ".asc"})
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func requireStrict(t *testing.T) {
	t.Helper()
	require.True(t, fips140.Enforced(), "test binary must run with GODEBUG=fips140=only")
}

func generateKey(t *testing.T, cfg handlertest.KeyConfig) *handlertest.Key {
	t.Helper()
	var key *handlertest.Key
	fips140.WithoutEnforcement(func() {
		var err error
		key, err = handlertest.GenerateKey(cfg)
		require.NoError(t, err)
	})
	return key
}

func mustHandler(t *testing.T) *handler.Handler {
	t.Helper()
	h, err := handler.New(nil)
	require.NoError(t, err)
	return h
}

func testDigest() descruntime.Digest {
	sum := crypto.SHA256.New()
	sum.Write([]byte("fips140-test-data"))
	return descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "jsonNormalisation/v4alpha1",
		Value:                  hex.EncodeToString(sum.Sum(nil)),
	}
}

func signed(sig descruntime.SignatureInfo) descruntime.Signature {
	return descruntime.Signature{Name: "test", Digest: testDigest(), Signature: sig}
}
