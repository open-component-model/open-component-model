// Package gpgbinary exports keys from the user's GnuPG keyring by invoking the GnuPG gpg binary.
// It performs no signing or verification. Every gpg invocation is bounded by a timeout of 3 minutes.
package gpgbinary

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
)

const (
	minimumVersion   = "2.2.0"
	operationTimeout = 3 * time.Minute
	versionTimeout   = 10 * time.Second
	maxStderr        = 4096
)

// ErrGPGNotFound is returned when no gpg binary is found on PATH.
var ErrGPGNotFound = errors.New(`reading keys from the GnuPG keyring (keyringFingerprint in the GPG credentials) requires the GnuPG "gpg" binary (>= 2.2.0) on PATH`)

var (
	gpgVersionRegexp       = regexp.MustCompile(`^gpg \(GnuPG[^)]*\) (\d+\.\d+\.\d+)`)
	libgcryptVersionRegexp = regexp.MustCompile(`(?m)^libgcrypt (\S+)`)
)

// ExecFunc runs binaryPath with args, feeding stdin if non-nil.
type ExecFunc func(ctx context.Context, binaryPath string, args []string, stdin []byte) (stdout, stderr []byte, err error)

// Option configures a Binary.
type Option func(*Binary)

// WithLookPath overrides how binaries are located on PATH.
func WithLookPath(fn func(file string) (string, error)) Option {
	return func(b *Binary) { b.lookPath = fn }
}

// WithExec overrides how binaries are executed.
func WithExec(fn ExecFunc) Option {
	return func(b *Binary) { b.exec = fn }
}

// Binary resolves and invokes the gpg binary. It is safe for concurrent use.
// Resolution is retried on every call until it succeeds once; the resolved path is cached afterwards.
type Binary struct {
	lookPath func(file string) (string, error)
	exec     ExecFunc

	mu      sync.Mutex
	gpgPath string // set after the first successful resolution
}

// New returns a Binary that resolves binaries via exec.LookPath and runs them as subprocesses.
func New(opts ...Option) *Binary {
	b := &Binary{lookPath: exec.LookPath, exec: execCommand}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// ExportPublicKey returns the ASCII-armored public key with the given full fingerprint from the keyring in home ("" = gpg's default).
func (b *Binary) ExportPublicKey(ctx context.Context, home, fingerprint string) ([]byte, error) {
	out, err := b.export(ctx, "export", home, nil, "--armor", "--export", fingerprint)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("key %s not found in the GnuPG keyring", fingerprint)
	}
	return out, nil
}

// ExportSecretKey returns the ASCII-armored secret key with the given full fingerprint. gpg-agent keeps its passphrase
// protection, so passphrase (passed on stdin, never argv) must unlock it for export and again in OCM.
func (b *Binary) ExportSecretKey(ctx context.Context, home, fingerprint, passphrase string) ([]byte, error) {
	out, err := b.export(ctx, "export-secret-keys", home, []byte(passphrase),
		"--pinentry-mode", "loopback", "--passphrase-fd", "0", "--armor", "--export-secret-keys", fingerprint)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no secret key %s found in the GnuPG keyring", fingerprint)
	}
	return out, nil
}

func (b *Binary) export(ctx context.Context, op, home string, stdin []byte, args ...string) ([]byte, error) {
	path, err := b.resolve(ctx)
	if err != nil {
		return nil, err
	}
	base := []string{"--batch", "--no-tty"}
	if home != "" {
		base = append(base, "--homedir", home)
	}
	return b.run(ctx, op, path, append(base, args...), stdin)
}

func (b *Binary) resolve(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.gpgPath != "" {
		return b.gpgPath, nil
	}

	path, err := b.lookPath("gpg")
	if err != nil {
		return "", ErrGPGNotFound
	}

	vctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, err := b.run(vctx, "version", path, []string{"--version"}, nil)
	if err != nil {
		return "", err
	}

	version, libgcrypt, err := parseVersion(string(out))
	if err != nil {
		slog.WarnContext(ctx, "could not parse gpg version; GnuPG >= 2.2.0 is required", "path", path, "error", err)
	} else {
		v, err := semver.NewVersion(version)
		if err != nil {
			return "", fmt.Errorf("parse gpg version %q: %w", version, err)
		}
		if v.LessThan(semver.MustParse(minimumVersion)) {
			return "", fmt.Errorf("gpg on PATH (%s) is version %s, minimum required is %s", path, version, minimumVersion)
		}
	}
	slog.DebugContext(ctx, "gpg resolved", "path", path, "version", version, "libgcrypt", libgcrypt)
	b.gpgPath = path
	return path, nil
}

// run invokes a gpg operation.
func (b *Binary) run(ctx context.Context, op, binaryPath string, args []string, stdin []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()

	// args never contain secrets: the passphrase is passed on stdin.
	slog.DebugContext(ctx, "gpg: invoking", "operation", op, "args", args)

	stdout, stderr, err := b.exec(ctx, binaryPath, args, stdin)
	if err != nil {
		msg := strings.TrimSpace(string(stderr))
		if len(msg) > maxStderr {
			msg = msg[:maxStderr] + " [truncated]"
		}
		if ctxErr := ctx.Err(); errors.Is(ctxErr, context.DeadlineExceeded) {
			return stdout, fmt.Errorf("gpg %s timed out: %w\nstderr: %s", op, errors.Join(ctxErr, err), msg)
		}
		return stdout, fmt.Errorf("gpg %s failed: %w\nstderr: %s", op, err, msg)
	}
	return stdout, nil
}

func execCommand(ctx context.Context, binaryPath string, args []string, stdin []byte) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// parseVersion extracts the GnuPG and (optional) libgcrypt versions from "gpg --version".
func parseVersion(out string) (gpg, libgcrypt string, err error) {
	firstLine, _, _ := strings.Cut(out, "\n")
	m := gpgVersionRegexp.FindStringSubmatch(strings.TrimSpace(firstLine))
	if m == nil {
		return "", "", fmt.Errorf("unrecognized gpg --version output %q", firstLine)
	}
	if lm := libgcryptVersionRegexp.FindStringSubmatch(out); lm != nil {
		libgcrypt = lm[1]
	}
	return m[1], libgcrypt, nil
}
