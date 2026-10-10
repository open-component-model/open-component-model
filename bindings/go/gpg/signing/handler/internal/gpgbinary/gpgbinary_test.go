package gpgbinary

import (
	"context"
	"errors"
	"os/exec"
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
				r.ErrorIs(err, ErrGPGNotFound)
				r.EqualError(err, `reading keys from the GnuPG keyring (keyringFingerprint in the GPG credentials) requires the GnuPG "gpg" binary (>= 2.2.0) on PATH`)
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

func TestBinary_RunTimeout(t *testing.T) {
	r := require.New(t)
	b := New(WithExec(func(ctx context.Context, _ string, _ []string, _ []byte) ([]byte, []byte, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}))
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	_, err := b.run(ctx, "sign", "/fake/bin/gpg", nil, nil)
	r.ErrorIs(err, context.DeadlineExceeded)
	r.ErrorContains(err, "gpg sign timed out")
}

func TestBinary_Export(t *testing.T) {
	const fpr = "0123456789ABCDEF0123456789ABCDEF01234567"
	secretArgs := []string{"--pinentry-mode", "loopback", "--passphrase-fd", "0", "--armor", "--export-secret-keys", fpr}

	tests := []struct {
		name       string
		secret     bool
		home       string
		passphrase string
		out        string
		execErr    error
		wantArgs   []string
		wantStdin  []byte
		wantErr    string
	}{
		{name: "public key", out: "key", wantArgs: []string{"--batch", "--no-tty", "--armor", "--export", fpr}},
		{name: "public key from home", home: "/h", out: "key", wantArgs: []string{"--batch", "--no-tty", "--homedir", "/h", "--armor", "--export", fpr}},
		{name: "secret key, passphrase on stdin", secret: true, passphrase: "s3cret", out: "key", wantArgs: append([]string{"--batch", "--no-tty"}, secretArgs...), wantStdin: []byte("s3cret")},
		{name: "secret key from home", secret: true, home: "/h", out: "key", wantArgs: append([]string{"--batch", "--no-tty", "--homedir", "/h"}, secretArgs...), wantStdin: []byte{}},
		{name: "unknown public key", wantErr: "key " + fpr + " not found in the GnuPG keyring"},
		{name: "unknown secret key", secret: true, wantErr: "no secret key " + fpr + " found in the GnuPG keyring"},
		{name: "gpg fails", secret: true, execErr: errors.New("exit status 2"), wantErr: "gpg export-secret-keys failed: exit status 2\nstderr: gpg: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var gotArgs []string
			var gotStdin []byte
			b := New(fakeLookPath, WithExec(func(_ context.Context, _ string, args []string, stdin []byte) ([]byte, []byte, error) {
				if args[0] == "--version" {
					return []byte("gpg (GnuPG) 2.4.4\n"), nil, nil
				}
				gotArgs, gotStdin = args, stdin
				return []byte(tt.out), []byte("gpg: boom"), tt.execErr
			}))

			var out []byte
			var err error
			if tt.secret {
				out, err = b.ExportSecretKey(t.Context(), tt.home, fpr, tt.passphrase)
			} else {
				out, err = b.ExportPublicKey(t.Context(), tt.home, fpr)
			}
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				r.Nil(out)
				return
			}
			r.NoError(err)
			r.Equal(tt.out, string(out))
			r.Equal(tt.wantArgs, gotArgs)
			r.Equal(tt.wantStdin, gotStdin)
		})
	}

	t.Run("gpg not on PATH", func(t *testing.T) {
		b := New(WithLookPath(func(string) (string, error) { return "", exec.ErrNotFound }))
		_, err := b.ExportSecretKey(t.Context(), "", fpr, "")
		require.ErrorIs(t, err, ErrGPGNotFound)
	})
}
