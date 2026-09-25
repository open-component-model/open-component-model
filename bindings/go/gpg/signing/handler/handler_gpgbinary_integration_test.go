package handler

import (
	"crypto"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/gpg/signing/handler/internal/gpgbinary"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

// Test_Integration_GPGHandler_GPGBinary exercises the FIPS 140-3 backend against a real
// GnuPG installation and checks that its signatures interoperate with the go-crypto backend.
func Test_Integration_GPGHandler_GPGBinary(t *testing.T) {
	_, err := exec.LookPath("gpg")
	require.NoError(t, err, "GnuPG >= 2.2 must be on PATH")

	fips := &Handler{fipsEnabled: func() bool { return true }, gpgBinary: gpgbinary.New()}
	plain := mustHandler(t)

	const passphrase = "pw"
	unprotected := mustEntity(t, "")
	protected := mustEntity(t, passphrase)
	other := mustEntity(t, "")
	sha256Digest := makeDigest(t, crypto.SHA256, []byte("gpg binary integration"))
	sha512Digest := makeDigest(t, crypto.SHA512, []byte("gpg binary integration"))

	signed := func(digest descruntime.Digest, sig descruntime.SignatureInfo) descruntime.Signature {
		return descruntime.Signature{Name: "test", Digest: digest, Signature: sig}
	}

	tests := []struct {
		name string
		run  func(t *testing.T, r *require.Assertions)
	}{
		{
			name: "unprotected key round trip",
			run: func(t *testing.T, r *require.Assertions) {
				sig, err := fips.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, armoredPrivKey(t, unprotected))
				r.NoError(err)
				r.Equal(v1alpha1.AlgorithmGPG, sig.Algorithm)
				r.Equal(v1alpha1.MediaTypeGPG, sig.MediaType)
				r.True(strings.HasPrefix(sig.Value, "-----BEGIN PGP SIGNATURE-----"), sig.Value)
				r.NoError(fips.Verify(t.Context(), signed(sha256Digest, sig), &v1alpha1.Config{}, armoredPubKey(t, unprotected)))
			},
		},
		{
			name: "passphrase-protected key round trip",
			run: func(t *testing.T, r *require.Assertions) {
				sig, err := fips.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, armoredPrivKeyWithPassphrase(t, protected, passphrase))
				r.NoError(err)
				r.NoError(fips.Verify(t.Context(), signed(sha256Digest, sig), &v1alpha1.Config{}, armoredPubKey(t, protected)))
			},
		},
		{
			name: "protected key with wrong passphrase",
			run: func(t *testing.T, r *require.Assertions) {
				_, err := fips.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, armoredPrivKeyWithPassphrase(t, protected, "wrong"))
				r.Error(err)
			},
		},
		{
			name: "protected key with empty passphrase",
			run: func(t *testing.T, r *require.Assertions) {
				_, err := fips.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, armoredPrivKey(t, protected))
				r.Error(err)
			},
		},
		{
			name: "gpg signature verifies with go-crypto",
			run: func(t *testing.T, r *require.Assertions) {
				sig, err := fips.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, armoredPrivKey(t, unprotected))
				r.NoError(err)
				r.NoError(plain.Verify(t.Context(), signed(sha256Digest, sig), &v1alpha1.Config{}, armoredPubKey(t, unprotected)))
			},
		},
		{
			name: "go-crypto signature verifies with gpg",
			run: func(t *testing.T, r *require.Assertions) {
				sig, err := plain.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, armoredPrivKey(t, unprotected))
				r.NoError(err)
				r.NoError(fips.Verify(t.Context(), signed(sha256Digest, sig), &v1alpha1.Config{}, armoredPubKey(t, unprotected)))
			},
		},
		{
			name: "verify with another entity's public key",
			run: func(t *testing.T, r *require.Assertions) {
				sig, err := fips.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, armoredPrivKey(t, unprotected))
				r.NoError(err)
				r.Error(fips.Verify(t.Context(), signed(sha256Digest, sig), &v1alpha1.Config{}, armoredPubKey(t, other)))
			},
		},
		{
			name: "SHA-512",
			run: func(t *testing.T, r *require.Assertions) {
				cfg := &v1alpha1.Config{HashAlgorithm: v1alpha1.HashAlgorithmSHA512}
				sig, err := fips.Sign(t.Context(), sha512Digest, cfg, armoredPrivKey(t, unprotected))
				r.NoError(err)
				r.NoError(plain.Verify(t.Context(), signed(sha512Digest, sig), cfg, armoredPubKey(t, unprotected)))
				r.NoError(fips.Verify(t.Context(), signed(sha512Digest, sig), cfg, armoredPubKey(t, unprotected)))
			},
		},
		{
			name: "key fingerprint selectors",
			run: func(t *testing.T, r *require.Assertions) {
				for _, fp := range []string{
					fmt.Sprintf("%X", unprotected.PrimaryKey.Fingerprint),
					fmt.Sprintf("%016X", unprotected.PrimaryKey.KeyId),
				} {
					cfg := &v1alpha1.Config{KeyFingerprint: fp}
					sig, err := fips.Sign(t.Context(), sha256Digest, cfg, armoredPrivKey(t, unprotected))
					r.NoError(err, fp)
					r.NoError(fips.Verify(t.Context(), signed(sha256Digest, sig), cfg, armoredPubKey(t, unprotected)), fp)
				}
			},
		},
		{
			name: "unknown key fingerprint on sign",
			run: func(t *testing.T, r *require.Assertions) {
				cfg := &v1alpha1.Config{KeyFingerprint: "DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF"}
				_, err := fips.Sign(t.Context(), sha256Digest, cfg, armoredPrivKey(t, unprotected))
				r.Error(err)
			},
		},
		{
			name: "key fingerprint mismatch on verify",
			run: func(t *testing.T, r *require.Assertions) {
				sig, err := fips.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, armoredPrivKey(t, other))
				r.NoError(err)
				pubs := &gpgcredentialsv1.GPGCredentials{
					PublicKeyPGP: armoredPubKey(t, unprotected).PublicKeyPGP + "\n" + armoredPubKey(t, other).PublicKeyPGP,
				}
				cfg := &v1alpha1.Config{KeyFingerprint: fmt.Sprintf("%X", unprotected.PrimaryKey.Fingerprint)}
				err = fips.Verify(t.Context(), signed(sha256Digest, sig), cfg, pubs)
				r.ErrorContains(err, "does not match the configured key fingerprint")
			},
		},
		{
			name: "public-only material as private key",
			run: func(t *testing.T, r *require.Assertions) {
				creds := &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: armoredPubKey(t, unprotected).PublicKeyPGP}
				_, err := fips.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, creds)
				r.ErrorContains(err, "no secret key found in private key material")
			},
		},
		{
			name: "certify-only primary key with signing subkey",
			run: func(t *testing.T, r *require.Assertions) {
				secret, public, primaryFpr := gpgCertifyOnlyKey(t)
				privCreds := &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: secret}
				pubCreds := &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: public}
				for _, fp := range []string{"", primaryFpr} {
					cfg := &v1alpha1.Config{KeyFingerprint: fp}
					sig, err := fips.Sign(t.Context(), sha256Digest, cfg, privCreds)
					r.NoError(err, fp)
					r.NoError(fips.Verify(t.Context(), signed(sha256Digest, sig), cfg, pubCreds), fp)
					r.NoError(plain.Verify(t.Context(), signed(sha256Digest, sig), cfg, pubCreds), fp)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t, require.New(t))
		})
	}
}

// gpgCertifyOnlyKey generates, with gpg itself, an unprotected key whose primary key can only
// certify and whose signing capability lives in a subkey.
func gpgCertifyOnlyKey(t *testing.T) (secretArmored, publicArmored, primaryFpr string) {
	t.Helper()
	r := require.New(t)
	// Not t.TempDir(): its long path can overflow the Unix socket path limit of gpg-agent on macOS.
	home, err := os.MkdirTemp("", "ocm-gpg-test-")
	r.NoError(err)
	t.Cleanup(func() {
		_ = exec.Command("gpgconf", "--homedir", home, "--kill", "all").Run()
		_ = os.RemoveAll(home)
	})

	gpg := func(args ...string) string {
		base := []string{"--batch", "--homedir", home, "--pinentry-mode", "loopback", "--passphrase", ""}
		out, err := exec.CommandContext(t.Context(), "gpg", append(base, args...)...).Output()
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		r.NoError(err, "gpg %v: %s", args, stderr)
		return string(out)
	}

	gpg("--quick-gen-key", "OCM Test <ocm-test@example.com>", "rsa3072", "cert", "never")
	// The first fpr record of a single-key listing belongs to the primary key.
	for line := range strings.SplitSeq(gpg("--with-colons", "--list-secret-keys"), "\n") {
		if fields := strings.Split(line, ":"); fields[0] == "fpr" && len(fields) > 9 {
			primaryFpr = fields[9]
			break
		}
	}
	r.NotEmpty(primaryFpr)
	gpg("--quick-add-key", primaryFpr, "rsa3072", "sign", "never")
	return gpg("--armor", "--export-secret-keys", primaryFpr), gpg("--armor", "--export", primaryFpr), primaryFpr
}
