package handler

import (
	"crypto"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler/internal/gpgbinary"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

// fipsHandlerWithoutGPG returns a handler in FIPS mode whose gpg lookup always fails.
func fipsHandlerWithoutGPG(t *testing.T) *Handler {
	t.Helper()
	h, err := New(v1alpha1.Scheme)
	require.NoError(t, err)
	h.fipsEnabled = func() bool { return true }
	h.gpgBinary.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	return h
}

func TestGPGHandler_FIPSMode_RequiresGPG(t *testing.T) {
	r := require.New(t)
	fips := fipsHandlerWithoutGPG(t)
	plain := mustHandler(t)
	entity := mustEntity(t, "")
	privCreds := armoredPrivKey(t, entity)
	digest := makeDigest(t, crypto.SHA256, []byte("fips routing"))

	_, err := fips.Sign(t.Context(), digest, &v1alpha1.Config{}, privCreds)
	r.ErrorIs(err, gpgbinary.ErrGPGNotFound)

	sig, err := plain.Sign(t.Context(), digest, &v1alpha1.Config{}, privCreds)
	r.NoError(err)

	signed := descruntime.Signature{Name: "test", Digest: digest, Signature: sig}
	err = fips.Verify(t.Context(), signed, &v1alpha1.Config{}, armoredPubKey(t, entity))
	r.ErrorIs(err, gpgbinary.ErrGPGNotFound)
}

func TestGPGHandler_FIPSMode_MissingKeys(t *testing.T) {
	r := require.New(t)
	fips := fipsHandlerWithoutGPG(t)
	digest := makeDigest(t, crypto.SHA256, []byte("missing keys"))

	_, err := fips.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{})
	r.ErrorIs(err, ErrMissingPrivateKey)

	signed := descruntime.Signature{
		Name:      "test",
		Digest:    digest,
		Signature: descruntime.SignatureInfo{MediaType: v1alpha1.MediaTypeGPG, Value: "irrelevant"},
	}
	err = fips.Verify(t.Context(), signed, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{})
	r.ErrorIs(err, ErrMissingPublicKey)
}

func TestGPGHandler_FIPSMode_InvalidHashAlgorithm(t *testing.T) {
	r := require.New(t)
	fips := fipsHandlerWithoutGPG(t)
	privCreds := armoredPrivKey(t, mustEntity(t, ""))
	digest := makeDigest(t, crypto.SHA256, []byte("hash alg test"))

	_, err := fips.Sign(t.Context(), digest, &v1alpha1.Config{HashAlgorithm: "SHA521"}, privCreds)
	r.ErrorContains(err, "SHA521")
}
