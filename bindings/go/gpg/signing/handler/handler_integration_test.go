package handler

import (
	"bytes"
	"cmp"
	"context"
	"crypto"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
	"github.com/stretchr/testify/require"

	gpgcredentialsv1 "ocm.software/open-component-model/bindings/go/gpg/spec/credentials/v1alpha1"
	"ocm.software/open-component-model/bindings/go/gpg/spec/signing/v1alpha1"
)

// Test_Integration_GPGHandler checks interoperability with a real GnuPG in both directions,
// with keys and signatures gpg created.
func Test_Integration_GPGHandler(t *testing.T) {
	requireGPG(t)
	h := mustHandler(t)

	withSubkey := gpgKey(t, "certify-only with signing subkey", "ed25519", "cert", "")
	withSubkey.addSubkey(t, "ed25519", "sign")
	keys := []*testKey{
		gpgKey(t, "ed25519", "ed25519", "sign", ""),
		gpgKey(t, "rsa3072", "rsa3072", "sign", ""),
		gpgKey(t, "nistp256", "nistp256", "sign", ""),
		gpgKey(t, "nistp384", "nistp384", "sign", ""),
		gpgKey(t, "protected ed25519", "ed25519", "sign", "pw"),
		withSubkey,
	}

	// Without hashAlgorithm, OCM and gpg each choose the hash for the key; SHA-512 suits every key.
	for _, hash := range []v1alpha1.HashAlgorithm{"", v1alpha1.HashAlgorithmSHA512} {
		digest := makeDigest(t, crypto.SHA256, []byte("gpg integration"))
		cfg := &v1alpha1.Config{HashAlgorithm: hash}
		gpgSignArgs := []string{"--armor", "--detach-sign", "--output", "-"}
		if hash != "" {
			gpgSignArgs = append(gpgSignArgs, "--digest-algo", strings.ReplaceAll(string(hash), "-", ""))
		}
		hashName := cmp.Or(string(hash), "default hash")

		for _, k := range keys {
			t.Run(fmt.Sprintf("OCM signs, gpg verifies: %s, %s", k.name, hashName), func(t *testing.T) {
				r := require.New(t)
				info, err := h.Sign(t.Context(), digest, cfg, k.privCreds())
				r.NoError(err)
				out := k.gpg(t, "--status-fd", "1", "--verify", k.file(t, "sig.asc", info.Value), k.digestFile(t, digest.Value))
				r.Contains(out, "[GNUPG:] GOODSIG")
			})

			t.Run(fmt.Sprintf("gpg signs, OCM verifies: %s, %s", k.name, hashName), func(t *testing.T) {
				r := require.New(t)
				sig := k.gpg(t, slices.Concat(gpgSignArgs, []string{k.digestFile(t, digest.Value)})...)
				r.NoError(h.Verify(t.Context(), gpgSignature(digest, sig), cfg, k.pubCreds()))
				r.NoError(h.Verify(t.Context(), gpgSignature(digest, sig), &v1alpha1.Config{KeyFingerprint: k.fpr}, k.pubCreds()))
			})
		}
	}

	// gpg --export-secret-subkeys replaces the primary secret key with a stub (GNU S2K extension "gnu-dummy"),
	// like a key whose primary key is kept offline.
	digest := makeDigest(t, crypto.SHA256, []byte("gpg integration"))
	t.Run("offline primary key: the signing subkey signs", func(t *testing.T) {
		r := require.New(t)
		subkeysOnly := withSubkey.gpg(t, "--armor", "--export-secret-subkeys", withSubkey.fpr)
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: subkeysOnly})
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{KeyFingerprint: withSubkey.fpr}, withSubkey.pubCreds()))
	})

	t.Run("offline primary key without signing subkey", func(t *testing.T) {
		k := gpgKey(t, "primary signs", "ed25519", "sign", "")
		k.addSubkey(t, "cv25519", "encr")
		subkeysOnly := k.gpg(t, "--armor", "--export-secret-subkeys", k.fpr)
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, &gpgcredentialsv1.GPGCredentials{PrivateKeyPGP: subkeysOnly})
		require.ErrorContains(t, err, "is a stub without secret key material")
	})

	// GnuPG reads LibrePGP v5 keys since 2.3 and creates PQC keys since 2.5.
	t.Run("LibrePGP v5 key", func(t *testing.T) {
		requireGPGVersion(t, "2.3.0")
		interop(t, h, importedKey(t, "librepgp-v5", readFixture(t, "gnupg/librepgp-v5.asc")))
	})
	t.Run("PQC key with Kyber encryption subkey", func(t *testing.T) {
		requireGPGVersion(t, "2.5.0")
		interop(t, h, gpgKey(t, "pqc", "pqc", "default", ""))
	})
}

// interop signs with OCM and verifies with gpg, and the other way round.
func interop(t *testing.T, h *Handler, k *testKey) {
	t.Helper()
	r := require.New(t)
	digest := makeDigest(t, crypto.SHA256, []byte("gpg integration"))
	info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, k.privCreds())
	r.NoError(err)
	r.Contains(k.gpg(t, "--status-fd", "1", "--verify", k.file(t, "sig.asc", info.Value), k.digestFile(t, digest.Value)), "[GNUPG:] GOODSIG")
	sig := k.gpg(t, "--armor", "--detach-sign", "--output", "-", k.digestFile(t, digest.Value))
	r.NoError(h.Verify(t.Context(), gpgSignature(digest, sig), &v1alpha1.Config{KeyFingerprint: k.fpr}, k.pubCreds()))
}

// Test_Integration_GPGHandler_Keyring signs and verifies with the keys of a GnuPG keyring
// selected through keyringFingerprint in the credentials.
func Test_Integration_GPGHandler_Keyring(t *testing.T) {
	requireGPG(t)
	h := mustHandler(t)
	digest := makeDigest(t, crypto.SHA256, []byte("keyring integration"))

	keyring := gpgKey(t, "keyring", "ed25519", "sign", "")
	protected := gpgKey(t, "protected", "rsa3072", "sign", "pw")
	other := gpgKey(t, "other", "ed25519", "sign", "")
	withSubkey := gpgKey(t, "with signing subkey", "ed25519", "cert", "")
	withSubkey.addSubkey(t, "nistp256", "sign")
	keyring.importKeys(t, protected.secret, other.public, withSubkey.secret)
	creds := func(fpr string) *gpgcredentialsv1.GPGCredentials {
		return &gpgcredentialsv1.GPGCredentials{KeyringFingerprint: fpr, KeyringHome: keyring.home}
	}

	t.Run("sign and verify", func(t *testing.T) {
		r := require.New(t)
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, creds(keyring.fpr))
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, creds(keyring.fpr)))
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, keyring.pubCreds()), "exported public key material")
		r.Contains(keyring.gpg(t, "--status-fd", "1", "--verify", keyring.file(t, "sig.asc", info.Value), keyring.digestFile(t, digest.Value)), "[GNUPG:] GOODSIG")
	})

	t.Run("GNUPGHOME selects the keyring when keyringHome is empty", func(t *testing.T) {
		r := require.New(t)
		t.Setenv("GNUPGHOME", keyring.home)
		c := &gpgcredentialsv1.GPGCredentials{KeyringFingerprint: keyring.fpr}
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, c)
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, c))
	})

	t.Run("fingerprint as printed by gpg --fingerprint", func(t *testing.T) {
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, creds(spaced(strings.ToLower(keyring.fpr))))
		require.NoError(t, err)
	})

	t.Run("protected key with passphrase", func(t *testing.T) {
		r := require.New(t)
		c := creds(protected.fpr)
		c.Passphrase = "pw"
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, c)
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, c))
	})

	for name, passphrase := range map[string]string{"without passphrase": "", "with wrong passphrase": "wrong"} {
		t.Run("protected key "+name, func(t *testing.T) {
			c := creds(protected.fpr)
			c.Passphrase = passphrase
			_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, c)
			require.ErrorContains(t, err, "export GPG private key from the GnuPG keyring")
		})
	}

	t.Run("subkey fingerprint signs with that subkey", func(t *testing.T) {
		r := require.New(t)
		subFpr := withSubkey.subkeyFingerprints(t)[0]
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, creds(subFpr))
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, creds(withSubkey.fpr)))
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, creds(subFpr)))
	})

	t.Run("config pin of another key", func(t *testing.T) {
		r := require.New(t)
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, creds(keyring.fpr))
		r.NoError(err)
		err = h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{KeyFingerprint: other.fpr}, creds(keyring.fpr))
		r.ErrorContains(err, "does not match the configured key fingerprint")
	})

	t.Run("signature by another key in the keyring", func(t *testing.T) {
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, other.privCreds())
		require.NoError(t, err)
		// Only the keyringFingerprint key is exported, so the other key in the keyring is unknown.
		err = h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, creds(keyring.fpr))
		require.ErrorIs(t, err, pgperrors.ErrUnknownIssuer)
	})

	t.Run("public key only in the keyring", func(t *testing.T) {
		_, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, creds(other.fpr))
		require.ErrorContains(t, err, fmt.Sprintf("no secret key %s found in the GnuPG keyring", other.fpr))
	})

	t.Run("key not in the keyring", func(t *testing.T) {
		outsider := gpgKey(t, "outsider", "ed25519", "sign", "")
		err := h.Verify(t.Context(), gpgSignature(digest, "irrelevant"), &v1alpha1.Config{}, creds(outsider.fpr))
		require.ErrorContains(t, err, fmt.Sprintf("key %s not found in the GnuPG keyring", outsider.fpr))
	})

	t.Run("revocation imported into the keyring", func(t *testing.T) {
		r := require.New(t)
		revoked := gpgKey(t, "revoked", "ed25519", "sign", "")
		keyring.importKeys(t, revoked.public)
		info, err := h.Sign(t.Context(), digest, &v1alpha1.Config{}, revoked.privCreds())
		r.NoError(err)
		r.NoError(h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, creds(revoked.fpr)))

		keyring.importKeys(t, revoked.revocationCertificate(t))
		err = h.Verify(t.Context(), gpgSignature(digest, info.Value), &v1alpha1.Config{}, creds(revoked.fpr))
		r.ErrorIs(err, pgperrors.ErrKeyRevoked)
	})
}

func requireGPG(t *testing.T) {
	t.Helper()
	_, err := exec.LookPath("gpg")
	require.NoError(t, err, "GnuPG >= 2.2 must be on PATH")
}

// requireGPGVersion skips the test if the gpg on PATH is older than minimum.
func requireGPGVersion(t *testing.T, minimum string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "gpg", "--version").Output()
	require.NoError(t, err)
	// The first line is "gpg (GnuPG) <version>".
	firstLine, _, _ := strings.Cut(string(out), "\n")
	fields := strings.Fields(firstLine)
	require.NotEmpty(t, fields)
	version := fields[len(fields)-1]
	if semver.MustParse(version).LessThan(semver.MustParse(minimum)) {
		t.Skipf("needs GnuPG >= %s, found %s", minimum, version)
	}
}

// testKey is an OpenPGP key generated by gpg in its own home directory.
type testKey struct {
	name, home, passphrase, fpr string
	secret, public              string
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
	k := &testKey{name: name, home: gpgHome(t), passphrase: passphrase}
	k.gpg(t, "--quick-gen-key", "OCM Test "+name+" <ocm-test@example.com>", algo, usage, "never")
	k.fpr = k.fingerprints(t)[0]
	k.export(t)
	return k
}

// importedKey imports an unprotected armored secret key into its own home directory.
func importedKey(t *testing.T, name, secret string) *testKey {
	t.Helper()
	k := &testKey{name: name, home: gpgHome(t), secret: secret}
	k.importKeys(t, secret)
	k.fpr = k.fingerprints(t)[0]
	k.public = k.gpg(t, "--armor", "--export", k.fpr)
	return k
}

func gpgHome(t *testing.T) string {
	t.Helper()
	// Not t.TempDir(): its long path can overflow the Unix socket path limit of gpg-agent on macOS.
	//nolint:usetesting // see above: deliberately using a short base path for gpg-agent sockets
	home, err := os.MkdirTemp("", "ocm-gpg-test-")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = exec.CommandContext(context.Background(), "gpgconf", "--homedir", home, "--kill", "all").Run()
		_ = os.RemoveAll(home)
	})
	return home
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

// subkeyFingerprints returns the fingerprints of k's subkeys.
func (k *testKey) subkeyFingerprints(t *testing.T) []string {
	t.Helper()
	return k.fingerprints(t)[1:]
}

// fingerprints returns the fingerprints of k's key, the primary key first, then the subkeys. Before k.fpr is set,
// it lists the only key in k's home directory.
func (k *testKey) fingerprints(t *testing.T) []string {
	t.Helper()
	args := []string{"--with-colons", "--with-subkey-fingerprints", "--list-keys"}
	if k.fpr != "" {
		args = append(args, k.fpr)
	}
	var fprs []string
	for line := range strings.SplitSeq(k.gpg(t, args...), "\n") {
		if fields := strings.Split(line, ":"); fields[0] == "fpr" && len(fields) > 9 {
			fprs = append(fprs, fields[9])
		}
	}
	require.NotEmpty(t, fprs)
	return fprs
}

func (k *testKey) importKeys(t *testing.T, keys ...string) {
	t.Helper()
	for i, key := range keys {
		k.gpg(t, "--import", k.file(t, fmt.Sprintf("import-%d.asc", i), key))
	}
}

// revocationCertificate returns the revocation certificate gpg generated for k, ready to import.
func (k *testKey) revocationCertificate(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(k.home, "openpgp-revocs.d", k.fpr+".rev"))
	require.NoError(t, err)
	// gpg prefixes the armor header with ":" so that the certificate is not imported by accident.
	return strings.ReplaceAll(string(b), ":-----BEGIN PGP PUBLIC KEY BLOCK-----", "-----BEGIN PGP PUBLIC KEY BLOCK-----")
}

// digestFile writes the digest bytes that OCM signs to a file for gpg.
func (k *testKey) digestFile(t *testing.T, hexDigest string) string {
	t.Helper()
	b, err := hex.DecodeString(hexDigest)
	require.NoError(t, err)
	return k.file(t, "digest.bin", string(b))
}

func (k *testKey) file(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
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
