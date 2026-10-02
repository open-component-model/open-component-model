package gpgbinary

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name      string
		out       string
		gpg       string
		libgcrypt string
		wantErr   bool
	}{
		{name: "gnupg with libgcrypt", out: "gpg (GnuPG) 2.4.4\nlibgcrypt 1.10.3\n", gpg: "2.4.4", libgcrypt: "1.10.3"},
		{name: "macgpg without libgcrypt", out: "gpg (GnuPG/MacGPG2) 2.2.41\n", gpg: "2.2.41"},
		{name: "libgcrypt suffix", out: "gpg (GnuPG) 2.5.22\nlibgcrypt 1.12.4-unknown", gpg: "2.5.22", libgcrypt: "1.12.4-unknown"},
		{name: "garbage", out: "garbage", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			gpg, libgcrypt, err := parseVersion(tt.out)
			if tt.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.Equal(tt.gpg, gpg)
			r.Equal(tt.libgcrypt, libgcrypt)
		})
	}
}

func TestFirstSecretKeyFingerprint(t *testing.T) {
	tests := []struct {
		name    string
		colons  string
		want    string
		wantErr string
	}{
		{
			name: "first primary key wins over subkeys and later keys",
			colons: `tru::1:1700000000:0:3:1:5
sec:u:3072:1:AAAAAAAAAAAAAAAA:1700000000:::u:::scESC:::+:::23::0:
fpr:::::::::1111111111111111111111111111111111111111:
grp:::::::::0123456789ABCDEF0123456789ABCDEF01234567:
uid:u::::1700000000::HASH::OCM Test <a@example.com>::::::::::0:
ssb:u:3072:1:BBBBBBBBBBBBBBBB:1700000000::::::s:::+:::23:
fpr:::::::::2222222222222222222222222222222222222222:
grp:::::::::0123456789ABCDEF0123456789ABCDEF01234567:
sec:u:3072:1:CCCCCCCCCCCCCCCC:1700000000:::u:::scESC:::+:::23::0:
fpr:::::::::3333333333333333333333333333333333333333:
ssb:u:3072:1:DDDDDDDDDDDDDDDD:1700000000::::::s:::+:::23:
fpr:::::::::4444444444444444444444444444444444444444:
`,
			want: "1111111111111111111111111111111111111111",
		},
		{
			name: "public keys only",
			colons: `pub:u:3072:1:AAAAAAAAAAAAAAAA:1700000000:::u:::scESC::::::23::0:
fpr:::::::::1111111111111111111111111111111111111111:
sub:u:3072:1:BBBBBBBBBBBBBBBB:1700000000::::::s::::::23:
fpr:::::::::2222222222222222222222222222222222222222:
`,
			wantErr: "no secret key found in private key material",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got, err := firstSecretKeyFingerprint(tt.colons)
			if tt.wantErr != "" {
				r.EqualError(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tt.want, got)
		})
	}
}

func TestParseVerifyStatus(t *testing.T) {
	const (
		signing = "2222222222222222222222222222222222222222"
		primary = "1111111111111111111111111111111111111111"
		other   = "3333333333333333333333333333333333333333"
	)
	validSig := func(fpr string) string {
		return "[GNUPG:] VALIDSIG " + fpr + " 2026-09-25 1790000000 0 4 0 1 8 00 " + primary + "\n"
	}
	tests := []struct {
		name    string
		status  string
		wantErr string
	}{
		{name: "good and valid", status: "[GNUPG:] NEWSIG\n[GNUPG:] GOODSIG AAAAAAAAAAAAAAAA OCM Test\n" + validSig(signing) + "[GNUPG:] TRUST_UNDEFINED 0 pgp\n"},
		{name: "valid only", status: validSig(signing), wantErr: "gpg reported no GOODSIG and VALIDSIG status"},
		{name: "good only", status: "[GNUPG:] GOODSIG AAAAAAAAAAAAAAAA OCM Test\n", wantErr: "gpg reported no GOODSIG and VALIDSIG status"},
		{name: "expired key", status: "[GNUPG:] EXPKEYSIG AAAAAAAAAAAAAAAA OCM Test\n" + validSig(signing), wantErr: "gpg reported EXPKEYSIG"},
		{
			// gpg exits 0 if one of several signatures is good; the good signature by another key
			// must not lend its GOODSIG to the revoked key's VALIDSIG.
			name: "good co-signature with a revoked key",
			status: "[GNUPG:] NEWSIG\n[GNUPG:] GOODSIG 3333333333333333 Other\n" + validSig(other) +
				"[GNUPG:] NEWSIG\n[GNUPG:] REVKEYSIG 2222222222222222 Pinned\n" + validSig(signing),
			wantErr: "gpg reported REVKEYSIG",
		},
		{
			name: "two good signatures",
			status: "[GNUPG:] NEWSIG\n[GNUPG:] GOODSIG 3333333333333333 Other\n" + validSig(other) +
				"[GNUPG:] NEWSIG\n[GNUPG:] GOODSIG 2222222222222222 Pinned\n" + validSig(signing),
			wantErr: "gpg reported 2 good signatures, but a GPG signature must contain exactly one",
		},
		{name: "bad signature next to a good one", status: "[GNUPG:] GOODSIG AAAAAAAAAAAAAAAA T\n" + validSig(signing) + "[GNUPG:] BADSIG BBBBBBBBBBBBBBBB T\n", wantErr: "gpg reported BADSIG"},
		{name: "empty", status: "", wantErr: "gpg reported no GOODSIG and VALIDSIG status"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			key, err := parseVerifyStatus(tt.status)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(verifiedKey{signing: signing, primary: primary}, key)
		})
	}
}

func TestFingerprintMatches(t *testing.T) {
	const fpr = "0123456789ABCDEF0123456789ABCDEF01234567"
	tests := []struct {
		name string
		want string
		ok   bool
	}{
		{name: "full lowercase", want: "0123456789abcdef0123456789abcdef01234567", ok: true},
		{name: "long key id", want: "89abcdef01234567", ok: true},
		{name: "short key id", want: "01234567", ok: false},
		{name: "different fingerprint", want: "FEDCBA9876543210FEDCBA9876543210FEDCBA98", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.New(t).Equal(tt.ok, fingerprintMatches(fpr, tt.want))
		})
	}
}

func TestBinary_Resolve(t *testing.T) {
	versionExec := func(out string) func(context.Context, string, []string, []byte) ([]byte, []byte, error) {
		return func(context.Context, string, []string, []byte) ([]byte, []byte, error) {
			return []byte(out), nil, nil
		}
	}
	tests := []struct {
		name     string
		lookPath func(string) (string, error)
		exec     func(context.Context, string, []string, []byte) ([]byte, []byte, error)
		check    func(r *require.Assertions, path string, err error)
	}{
		{
			name:     "gpg missing",
			lookPath: func(string) (string, error) { return "", exec.ErrNotFound },
			check: func(r *require.Assertions, _ string, err error) {
				r.True(errors.Is(err, ErrGPGNotFound))
				r.EqualError(err, `GPG signing requires the GnuPG "gpg" binary (>= 2.2.0) on PATH; install GnuPG, in FIPS 140-3 mode one backed by a FIPS 140-3 validated libgcrypt`)
			},
		},
		{
			name:     "gpg too old",
			lookPath: func(file string) (string, error) { return "/fake/bin/" + file, nil },
			exec:     versionExec("gpg (GnuPG) 2.1.23\nlibgcrypt 1.8.5\n"),
			check: func(r *require.Assertions, _ string, err error) {
				r.EqualError(err, "gpg on PATH (/fake/bin/gpg) is version 2.1.23, minimum required is 2.2.0")
			},
		},
		{
			name:     "unparseable version is tolerated",
			lookPath: func(file string) (string, error) { return "/fake/bin/" + file, nil },
			exec:     versionExec("weird"),
			check: func(r *require.Assertions, path string, err error) {
				r.NoError(err)
				r.Equal("/fake/bin/gpg", path)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []Option{WithLookPath(tt.lookPath)}
			if tt.exec != nil {
				opts = append(opts, WithExec(tt.exec))
			}
			b := New(opts...)
			path, err := b.resolve(t.Context())
			tt.check(require.New(t), path, err)
		})
	}
}

// TestBinary_KeyringInvocations pins what keyring mode must never do: switch to an isolated
// home, import key material, or fetch keys from the network during verification.
func TestBinary_KeyringInvocations(t *testing.T) {
	const fpr = "0123456789ABCDEF0123456789ABCDEF01234567"
	status := "[GNUPG:] GOODSIG 89ABCDEF01234567 T\n[GNUPG:] VALIDSIG " + fpr + " 2026-09-25 1 0 4 0 22 8 00 " + fpr + "\n"

	tests := []struct {
		name      string
		run       func(t *testing.T, b *Binary) error
		wantArgs  []string
		denyArgs  []string
		wantStdin []byte
	}{
		{
			name: "sign with agent unlocking",
			run: func(t *testing.T, b *Binary) error {
				_, err := b.Sign(t.Context(), SignRequest{UseKeyring: true, KeyFingerprint: fpr, DigestAlgo: "SHA256", Data: []byte("d")})
				return err
			},
			wantArgs: []string{"--local-user", fpr, "--detach-sign"},
			denyArgs: []string{"--homedir", "--pinentry-mode", "--passphrase-fd"},
		},
		{
			name: "sign with passphrase",
			run: func(t *testing.T, b *Binary) error {
				_, err := b.Sign(t.Context(), SignRequest{UseKeyring: true, Passphrase: "pw", DigestAlgo: "SHA256", Data: []byte("d")})
				return err
			},
			wantArgs:  []string{"--pinentry-mode", "loopback", "--passphrase-fd", "0"},
			denyArgs:  []string{"--homedir", "--local-user"},
			wantStdin: []byte("pw"),
		},
		{
			name: "verify",
			run: func(t *testing.T, b *Binary) error {
				return b.Verify(t.Context(), VerifyRequest{UseKeyring: true, KeyFingerprint: fpr, Data: []byte("d"), Signature: "sig"})
			},
			wantArgs: []string{"--no-auto-key-retrieve", "--verify"},
			denyArgs: []string{"--homedir"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var calls [][]string
			var stdin []byte
			b := New(fakeLookPath, WithExec(func(_ context.Context, _ string, args []string, in []byte) ([]byte, []byte, error) {
				if args[0] == "--version" {
					return []byte("gpg (GnuPG) 2.4.4\n"), nil, nil
				}
				calls = append(calls, args)
				stdin = in
				if slices.Contains(args, "--verify") {
					return []byte(status), nil, nil
				}
				return []byte("-----BEGIN PGP SIGNATURE-----"), nil, nil
			}))
			r.NoError(tt.run(t, b))
			r.Len(calls, 1, "keyring mode runs exactly one gpg operation: no import, no key listing, no agent shutdown")
			for _, want := range tt.wantArgs {
				r.Contains(calls[0], want)
			}
			for _, deny := range tt.denyArgs {
				r.NotContains(calls[0], deny)
			}
			r.Equal(tt.wantStdin, stdin)
		})
	}
}

var fakeLookPath = WithLookPath(func(file string) (string, error) { return "/fake/bin/" + file, nil })

func TestBinary_ResolveCaching(t *testing.T) {
	r := require.New(t)
	var lookups, versions int
	found := false
	b := New(
		WithLookPath(func(file string) (string, error) {
			lookups++
			if !found {
				return "", exec.ErrNotFound
			}
			return "/fake/bin/" + file, nil
		}),
		WithExec(func(context.Context, string, []string, []byte) ([]byte, []byte, error) {
			versions++
			return []byte("gpg (GnuPG) 2.4.4\n"), nil, nil
		}),
	)

	_, err := b.resolve(t.Context())
	r.ErrorIs(err, ErrGPGNotFound)
	found = true
	path, err := b.resolve(t.Context())
	r.NoError(err, "a failed lookup must not be cached")
	r.Equal("/fake/bin/gpg", path)

	lookupsAfterResolve, versionsAfterResolve := lookups, versions
	path, err = b.resolve(t.Context())
	r.NoError(err)
	r.Equal("/fake/bin/gpg", path)
	r.Equal(lookupsAfterResolve, lookups, "a resolved gpg must not be looked up again")
	r.Equal(versionsAfterResolve, versions, "a resolved gpg must not be version-checked again")
}

// TestBinary_IsolatedHomeCleanup pins that the isolated home directory, which holds the imported
// private key, has its gpg-agent stopped and is removed even when the operation's context is cancelled.
func TestBinary_IsolatedHomeCleanup(t *testing.T) {
	r := require.New(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var home string
	var killArgs []string
	var killCtxErr error
	b := New(fakeLookPath, WithExec(func(ctx context.Context, binaryPath string, args []string, _ []byte) ([]byte, []byte, error) {
		switch {
		case args[0] == "--version":
			return []byte("gpg (GnuPG) 2.4.4\n"), nil, nil
		case binaryPath == "/fake/bin/gpgconf":
			killArgs, killCtxErr = args, ctx.Err()
			return nil, nil, nil
		case slices.Contains(args, "--import"):
			home = args[slices.Index(args, "--homedir")+1]
			r.DirExists(home)
			cancel()
			return nil, []byte("interrupted"), context.Canceled
		}
		return nil, nil, errors.New("unexpected gpg invocation")
	}))

	_, err := b.Sign(ctx, SignRequest{PrivateKey: []byte("key"), DigestAlgo: "SHA256", Data: []byte("d")})
	r.ErrorIs(err, context.Canceled)
	r.Equal([]string{"--homedir", home, "--kill", "all"}, killArgs)
	r.NoError(killCtxErr, "gpg-agent must be stopped although the operation's context is cancelled")
	r.NoDirExists(home)
}

func TestBinary_RejectsKeyMaterialWithKeyring(t *testing.T) {
	r := require.New(t)
	b := New(WithExec(func(context.Context, string, []string, []byte) ([]byte, []byte, error) {
		return nil, nil, errors.New("gpg must not be invoked")
	}))
	_, err := b.Sign(t.Context(), SignRequest{UseKeyring: true, PrivateKey: []byte("key")})
	r.ErrorIs(err, ErrKeyMaterialWithKeyring)
	err = b.Verify(t.Context(), VerifyRequest{UseKeyring: true, KeyFingerprint: strings.Repeat("A", 40), PublicKey: []byte("key")})
	r.ErrorIs(err, ErrKeyMaterialWithKeyring)
}

func TestBinary_RunTimeout(t *testing.T) {
	r := require.New(t)
	b := New(WithExec(func(ctx context.Context, _ string, _ []string, _ []byte) ([]byte, []byte, error) {
		<-ctx.Done()
		return nil, nil, errors.New("signal: killed")
	}))
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	_, err := b.run(ctx, "sign", "/fake/bin/gpg", nil, nil)
	r.ErrorIs(err, context.DeadlineExceeded)
	r.ErrorContains(err, "gpg sign timed out")
	r.ErrorContains(err, "signal: killed")
}

func TestBinary_MkdirTemp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gpg-agent uses no Unix sockets in the home directory on Windows")
	}
	// Under /tmp, not t.TempDir() or $TMPDIR: a base nested there can already be too long for gpg-agent sockets.
	short, err := os.MkdirTemp(shortTempBase, "ocm-gpg-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	require.True(t, socketPathFits(short), "test base %q must be short", short)
	long := filepath.Join(short, strings.Repeat("d", 80))
	require.NoError(t, os.MkdirAll(long, 0o700))

	tests := []struct {
		name         string
		tempDir      string
		tmpdirEnv    string
		hostsSockets bool
		wantParent   string
	}{
		{name: "configured temp dir hosts a GnuPG home", tempDir: short, hostsSockets: true, wantParent: short},
		{name: "unset temp dir falls back to TMPDIR", tmpdirEnv: short, hostsSockets: true, wantParent: short},
		{name: "too long for sockets moves the GnuPG home to /tmp", tempDir: long, hostsSockets: true, wantParent: shortTempBase},
		{name: "keyring scratch dir stays in a long temp dir", tempDir: long, hostsSockets: false, wantParent: long},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			if tt.tmpdirEnv != "" {
				t.Setenv("TMPDIR", tt.tmpdirEnv)
			}
			dir, err := New(WithTempDir(tt.tempDir)).mkdirTemp(t.Context(), tt.hostsSockets)
			r.NoError(err)
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			r.Equal(tt.wantParent, filepath.Dir(dir))
		})
	}
}
