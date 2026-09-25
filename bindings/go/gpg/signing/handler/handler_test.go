package handler

import (
	"crypto"
	_ "crypto/sha256" // registers crypto.SHA256 for makeDigest
	_ "crypto/sha512" // registers crypto.SHA384 and crypto.SHA512 for makeDigest
	"encoding/hex"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler/internal/gpgbinary"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	identityv1 "ocm.software/open-component-model/bindings/go/gpg/spec/identity/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

// Key material is never parsed by the handler itself, so these tests only need placeholder keys.
const placeholderKey = "-----BEGIN PGP PRIVATE KEY BLOCK-----\n\n-----END PGP PRIVATE KEY BLOCK-----\n"

func TestGPGHandler_RequiresGPG(t *testing.T) {
	r := require.New(t)
	h := handlerWithoutGPG(t)
	digest := makeDigest(t, crypto.SHA256, []byte("no gpg"))

	_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: placeholderKey})
	r.ErrorIs(err, gpgbinary.ErrGPGNotFound)

	signed := gpgSignature(digest, "irrelevant")
	err = h.Verify(t.Context(), signed, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: placeholderKey})
	r.ErrorIs(err, gpgbinary.ErrGPGNotFound)
}

func TestGPGHandler_MissingKeys(t *testing.T) {
	r := require.New(t)
	h := handlerWithoutGPG(t)
	digest := makeDigest(t, crypto.SHA256, []byte("missing keys"))

	_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{})
	r.ErrorIs(err, ErrMissingPrivateKey)
	_, err = h.Sign(t.Context(), digest, &v1alpha1.Config{}, nil)
	r.ErrorIs(err, ErrMissingPrivateKey)

	err = h.Verify(t.Context(), gpgSignature(digest, "irrelevant"), &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{})
	r.ErrorIs(err, ErrMissingPublicKey)
}

func TestGPGHandler_InvalidInput(t *testing.T) {
	h := handlerWithoutGPG(t)
	creds := &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: placeholderKey}
	valid := makeDigest(t, crypto.SHA256, []byte("invalid input"))

	tests := []struct {
		name    string
		digest  descruntime.Digest
		cfg     *v1alpha1.Config
		wantErr error
		wantMsg string
	}{
		{name: "unsupported config hash algorithm", digest: valid, cfg: &v1alpha1.Config{HashAlgorithm: "SHA521"}, wantMsg: "SHA521"},
		{name: "missing digest hash algorithm", digest: descruntime.Digest{Value: valid.Value}, cfg: &v1alpha1.Config{}, wantErr: ErrMissingHashAlg},
		{name: "missing digest value", digest: descruntime.Digest{HashAlgorithm: "SHA-256"}, cfg: &v1alpha1.Config{}, wantErr: ErrMissingDigestVal},
		{name: "unsupported digest hash algorithm", digest: descruntime.Digest{HashAlgorithm: "MD5", Value: valid.Value}, cfg: &v1alpha1.Config{}, wantMsg: `unsupported hash algorithm "MD5"`},
		{name: "non-hex digest", digest: descruntime.Digest{HashAlgorithm: "SHA-256", Value: "zz"}, cfg: &v1alpha1.Config{}, wantMsg: "invalid hex digest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			_, err := h.Sign(t.Context(), tt.digest, tt.cfg, creds)
			if tt.wantErr != nil {
				r.ErrorIs(err, tt.wantErr)
			}
			if tt.wantMsg != "" {
				r.ErrorContains(err, tt.wantMsg)
			}
			r.NotErrorIs(err, gpgbinary.ErrGPGNotFound, "input must be validated before gpg is invoked")
		})
	}
}

func TestGPGHandler_Verify_UnsupportedMediaType(t *testing.T) {
	r := require.New(t)
	h := handlerWithoutGPG(t)
	signed := gpgSignature(makeDigest(t, crypto.SHA256, []byte("media type")), "irrelevant")
	signed.Signature.MediaType = "application/vnd.ocm.signature.rsa"

	err := h.Verify(t.Context(), signed, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: placeholderKey})
	r.ErrorContains(err, `unsupported media type "application/vnd.ocm.signature.rsa"`)
}

func TestGPGHandler_CredentialIdentities(t *testing.T) {
	r := require.New(t)
	h := mustHandler(t)
	digest := makeDigest(t, crypto.SHA256, []byte("data"))

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
	h.gpgBinary.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	return h
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
