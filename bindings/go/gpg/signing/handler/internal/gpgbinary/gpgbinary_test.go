package gpgbinary

import (
	"context"
	"errors"
	"os/exec"
	"testing"

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
	const validSig = "[GNUPG:] VALIDSIG 2222222222222222222222222222222222222222 2026-09-25 1790000000 0 4 0 1 8 00 1111111111111111111111111111111111111111"
	tests := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{name: "good and valid", status: "[GNUPG:] NEWSIG\n[GNUPG:] GOODSIG AAAAAAAAAAAAAAAA OCM Test\n" + validSig + "\n[GNUPG:] TRUST_UNDEFINED 0 pgp\n"},
		{name: "valid only", status: validSig + "\n", wantErr: true},
		{name: "expired key", status: "[GNUPG:] EXPKEYSIG AAAAAAAAAAAAAAAA OCM Test\n" + validSig + "\n", wantErr: true},
		{name: "empty", status: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			fields, err := parseVerifyStatus(tt.status)
			if tt.wantErr {
				r.ErrorContains(err, "gpg reported no GOODSIG and VALIDSIG status")
				return
			}
			r.NoError(err)
			r.Len(fields, 10)
			r.Equal("2222222222222222222222222222222222222222", fields[0])
			r.Equal("1111111111111111111111111111111111111111", fields[9])
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
			b := New()
			b.LookPath = tt.lookPath
			if tt.exec != nil {
				b.Exec = tt.exec
			}
			path, err := b.resolve(t.Context())
			tt.check(require.New(t), path, err)
		})
	}
}
