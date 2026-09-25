package handler

import (
	"bytes"
	"crypto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

// Test_Integration_GPGHandler signs and verifies against a real GnuPG installation.
func Test_Integration_GPGHandler(t *testing.T) {
	_, err := exec.LookPath("gpg")
	require.NoError(t, err, "GnuPG >= 2.2 must be on PATH")

	h := mustHandler(t)

	const passphrase = "pw"
	signer := gpgKey(t, "signer", "ed25519", "sign", "")
	rsaKey := gpgKey(t, "rsa", "rsa3072", "sign", "")
	protected := gpgKey(t, "protected", "ed25519", "sign", passphrase)
	other := gpgKey(t, "other", "ed25519", "sign", "")
	certifyOnly := gpgKey(t, "certify-only", "ed25519", "cert", "")
	certifyOnly.addSubkey(t, "ed25519", "sign")

	sha256Digest := makeDigest(t, crypto.SHA256, []byte("gpg integration"))

	roundTrip := func(t *testing.T, r *require.Assertions, digest descruntime.Digest, cfg *v1alpha1.Config, priv, pub *gpgcredentialsv1.GPGCredentials) {
		t.Helper()
		sig, err := h.Sign(t.Context(), digest, cfg, priv)
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, sig.Value), cfg, pub))
	}

	tests := []struct {
		name string
		run  func(t *testing.T, r *require.Assertions)
	}{
		{
			name: "signature format",
			run: func(t *testing.T, r *require.Assertions) {
				sig, err := h.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, signer.privCreds())
				r.NoError(err)
				r.Equal(v1alpha1.AlgorithmGPG, sig.Algorithm)
				r.Equal(v1alpha1.MediaTypeGPG, sig.MediaType)
				r.True(strings.HasPrefix(sig.Value, "-----BEGIN PGP SIGNATURE-----"), sig.Value)
			},
		},
		{
			name: "RSA key with every hash algorithm",
			run: func(t *testing.T, r *require.Assertions) {
				for alg, hash := range map[v1alpha1.HashAlgorithm]crypto.Hash{
					v1alpha1.HashAlgorithmSHA256: crypto.SHA256,
					v1alpha1.HashAlgorithmSHA384: crypto.SHA384,
					v1alpha1.HashAlgorithmSHA512: crypto.SHA512,
				} {
					digest := makeDigest(t, hash, []byte("gpg integration"))
					roundTrip(t, r, digest, &v1alpha1.Config{HashAlgorithm: alg}, rsaKey.privCreds(), rsaKey.pubCreds())
				}
			},
		},
		{
			name: "passphrase-protected key",
			run: func(t *testing.T, r *require.Assertions) {
				roundTrip(t, r, sha256Digest, &v1alpha1.Config{}, protected.privCreds(), protected.pubCreds())
			},
		},
		{
			name: "protected key with wrong passphrase",
			run: func(t *testing.T, r *require.Assertions) {
				creds := protected.privCreds()
				creds.Passphrase = "wrong"
				_, err := h.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, creds)
				r.Error(err)
			},
		},
		{
			name: "protected key without passphrase",
			run: func(t *testing.T, r *require.Assertions) {
				creds := protected.privCreds()
				creds.Passphrase = ""
				_, err := h.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, creds)
				r.Error(err)
			},
		},
		{
			name: "public key derived from private key material",
			run: func(t *testing.T, r *require.Assertions) {
				roundTrip(t, r, sha256Digest, &v1alpha1.Config{}, signer.privCreds(), signer.privCreds())
			},
		},
		{
			name: "wrong public key",
			run: func(t *testing.T, r *require.Assertions) {
				sig, err := h.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, signer.privCreds())
				r.NoError(err)
				r.Error(h.Verify(t.Context(), gpgSignature(sha256Digest, sig.Value), &v1alpha1.Config{}, other.pubCreds()))
			},
		},
		{
			name: "tampered digest",
			run: func(t *testing.T, r *require.Assertions) {
				sig, err := h.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, signer.privCreds())
				r.NoError(err)
				tampered := makeDigest(t, crypto.SHA256, []byte("tampered"))
				r.Error(h.Verify(t.Context(), gpgSignature(tampered, sig.Value), &v1alpha1.Config{}, signer.pubCreds()))
			},
		},
		{
			name: "key fingerprint selectors",
			run: func(t *testing.T, r *require.Assertions) {
				for _, fp := range []string{signer.fpr, strings.ToLower(signer.fpr), signer.fpr[len(signer.fpr)-16:]} {
					roundTrip(t, r, sha256Digest, &v1alpha1.Config{KeyFingerprint: fp}, signer.privCreds(), signer.pubCreds())
				}
			},
		},
		{
			name: "unknown key fingerprint on sign",
			run: func(t *testing.T, r *require.Assertions) {
				cfg := &v1alpha1.Config{KeyFingerprint: "DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF"}
				_, err := h.Sign(t.Context(), sha256Digest, cfg, signer.privCreds())
				r.Error(err)
			},
		},
		{
			name: "key fingerprint mismatch on verify",
			run: func(t *testing.T, r *require.Assertions) {
				sig, err := h.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, other.privCreds())
				r.NoError(err)
				keyring := &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: signer.public + "\n" + other.public}
				cfg := &v1alpha1.Config{KeyFingerprint: signer.fpr}
				err = h.Verify(t.Context(), gpgSignature(sha256Digest, sig.Value), cfg, keyring)
				r.ErrorContains(err, "does not match the configured key fingerprint")
			},
		},
		{
			name: "public-only material as private key",
			run: func(t *testing.T, r *require.Assertions) {
				creds := &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: signer.public}
				_, err := h.Sign(t.Context(), sha256Digest, &v1alpha1.Config{}, creds)
				r.ErrorContains(err, "no secret key found in private key material")
			},
		},
		{
			name: "certify-only primary key with signing subkey",
			run: func(t *testing.T, r *require.Assertions) {
				for _, fp := range []string{"", certifyOnly.fpr} {
					roundTrip(t, r, sha256Digest, &v1alpha1.Config{KeyFingerprint: fp}, certifyOnly.privCreds(), certifyOnly.pubCreds())
				}
			},
		},
		{
			name: "signatures of the former go-crypto implementation still verify",
			run: func(t *testing.T, r *require.Assertions) {
				pub := &gpgcredentialsv1.GPGCredentials{PublicKeyPGPFile: filepath.Join("testdata", "gocrypto", "public.asc")}
				for name, hashAlg := range map[string]string{"sha256": "SHA-256", "sha512": "SHA-512"} {
					digest := readFixture(t, name+".digest")
					sig := readFixture(t, name+".sig.asc")
					d := descruntime.Digest{HashAlgorithm: hashAlg, Value: digest}
					r.NoError(h.Verify(t.Context(), gpgSignature(d, sig), &v1alpha1.Config{}, pub), name)
					r.Error(h.Verify(t.Context(), gpgSignature(sha256Digest, sig), &v1alpha1.Config{}, pub), name)
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

// testKey is an OpenPGP key generated by gpg in its own home directory.
type testKey struct {
	home, passphrase, fpr string
	secret, public        string
}

func (k *testKey) privCreds() *gpgcredentialsv1.GPGCredentials {
	return &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: k.secret, Passphrase: k.passphrase}
}

func (k *testKey) pubCreds() *gpgcredentialsv1.GPGCredentials {
	return &gpgcredentialsv1.GPGCredentials{PublicKeyPGP: k.public}
}

// gpgKey generates a key whose primary key has the given algorithm and usage.
func gpgKey(t *testing.T, name, algo, usage, passphrase string) *testKey {
	t.Helper()
	// Not t.TempDir(): its long path can overflow the Unix socket path limit of gpg-agent on macOS.
	home, err := os.MkdirTemp("", "ocm-gpg-test-")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = exec.Command("gpgconf", "--homedir", home, "--kill", "all").Run()
		_ = os.RemoveAll(home)
	})
	k := &testKey{home: home, passphrase: passphrase}
	k.gpg(t, "--quick-gen-key", "OCM Test "+name+" <ocm-test@example.com>", algo, usage, "never")
	// The first fpr record of a single-key listing belongs to the primary key.
	for line := range strings.SplitSeq(k.gpg(t, "--with-colons", "--list-secret-keys"), "\n") {
		if fields := strings.Split(line, ":"); fields[0] == "fpr" && len(fields) > 9 {
			k.fpr = fields[9]
			break
		}
	}
	require.NotEmpty(t, k.fpr)
	k.export(t)
	return k
}

func (k *testKey) addSubkey(t *testing.T, algo, usage string) {
	t.Helper()
	k.gpg(t, "--quick-add-key", k.fpr, algo, usage, "never")
	k.export(t)
}

func (k *testKey) export(t *testing.T) {
	t.Helper()
	k.secret = k.gpg(t, "--armor", "--export-secret-keys", k.fpr)
	k.public = k.gpg(t, "--armor", "--export", k.fpr)
}

func (k *testKey) gpg(t *testing.T, args ...string) string {
	t.Helper()
	base := []string{"--batch", "--homedir", k.home, "--pinentry-mode", "loopback", "--passphrase", k.passphrase}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(t.Context(), "gpg", append(base, args...)...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, "gpg %v: %s", args, stderr.String())
	return string(out)
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "gocrypto", name))
	require.NoError(t, err)
	return strings.TrimSpace(string(b))
}
