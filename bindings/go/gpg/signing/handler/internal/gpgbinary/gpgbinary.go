// Package gpgbinary signs and verifies OpenPGP detached signatures by invoking
// the GnuPG "gpg" binary, so that all OpenPGP cryptography, including passphrase
// unwrapping, runs in the system libgcrypt. With a FIPS 140-3 validated libgcrypt
// this keeps GPG signing compliant in FIPS 140-3 mode.
//
// Every operation runs in a fresh temporary GnuPG home directory that only
// contains the key material of the request and is removed afterwards.
package gpgbinary

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
)

const (
	minimumVersion          = "2.2.0"
	defaultOperationTimeout = 3 * time.Minute
	versionTimeout          = 10 * time.Second
	cleanupTimeout          = 10 * time.Second
	maxStderr               = 4096
)

// ErrGPGNotFound is returned when no gpg binary is found on PATH.
var ErrGPGNotFound = errors.New(`GPG signing requires the GnuPG "gpg" binary (>= 2.2.0) on PATH; install GnuPG, in FIPS 140-3 mode one backed by a FIPS 140-3 validated libgcrypt`)

var (
	gpgVersionRegexp       = regexp.MustCompile(`^gpg \(GnuPG[^)]*\) (\d+\.\d+\.\d+)`)
	libgcryptVersionRegexp = regexp.MustCompile(`(?m)^libgcrypt (\S+)`)
)

// Binary resolves and invokes the gpg binary.
// Resolution is retried on every call until it succeeds once; the resolved path is cached afterwards.
type Binary struct {
	mu          sync.Mutex
	gpgPath     string // set after the first successful resolution
	gpgconfPath string // "" if gpgconf is not on PATH

	// OperationTimeout bounds a single gpg invocation; zero means defaultOperationTimeout.
	OperationTimeout time.Duration
	// LookPath locates binaries on PATH.
	LookPath func(file string) (string, error)
	// Exec runs binaryPath with args, feeding stdin if non-nil.
	Exec func(ctx context.Context, binaryPath string, args []string, stdin []byte) (stdout, stderr []byte, err error)
}

// New returns a Binary that resolves gpg via exec.LookPath.
func New() *Binary {
	b := &Binary{LookPath: exec.LookPath}
	b.Exec = b.exec
	return b
}

// SignRequest describes a detached signing operation.
type SignRequest struct {
	PrivateKey     []byte // armored or binary key material
	Passphrase     string
	KeyFingerprint string // "" selects the first secret key
	DigestAlgo     string // "SHA256" | "SHA384" | "SHA512"
	Data           []byte // hex-decoded digest bytes
}

// VerifyRequest describes a detached signature verification.
type VerifyRequest struct {
	PublicKey      []byte
	KeyFingerprint string // "" accepts any key in PublicKey
	Data           []byte
	Signature      string
}

// Sign returns an ASCII-armored detached signature over req.Data.
func (b *Binary) Sign(ctx context.Context, req SignRequest) (string, error) {
	gpgPath, err := b.resolve(ctx)
	if err != nil {
		return "", err
	}
	dir, cleanup, err := b.newHome(ctx)
	if err != nil {
		return "", err
	}
	defer cleanup()

	if _, err := b.run(ctx, "import", gpgPath, homeArgs(dir, "--import"), req.PrivateKey); err != nil {
		return "", err
	}

	// No "!" suffix: gpg picks the signing-capable (sub)key of the selected primary key.
	selector := req.KeyFingerprint
	if selector == "" {
		out, err := b.run(ctx, "list-secret-keys", gpgPath, homeArgs(dir, "--list-secret-keys", "--with-colons"), nil)
		if err != nil {
			return "", err
		}
		if selector, err = firstSecretKeyFingerprint(string(out)); err != nil {
			return "", err
		}
	}

	digestPath := filepath.Join(dir, "digest.bin")
	if err := os.WriteFile(digestPath, req.Data, 0o600); err != nil {
		return "", fmt.Errorf("write data to sign: %w", err)
	}

	// The passphrase is passed on stdin, never argv; an unprotected key never reads it.
	args := homeArgs(dir,
		"--pinentry-mode", "loopback",
		"--passphrase-fd", "0",
		"--local-user", selector,
		"--digest-algo", req.DigestAlgo,
		"--armor", "--detach-sign",
		"--output", "-",
		digestPath,
	)
	out, err := b.run(ctx, "sign", gpgPath, args, []byte(req.Passphrase))
	if err != nil {
		return "", err
	}
	if len(out) == 0 {
		return "", errors.New("gpg produced no signature output")
	}
	return string(out), nil
}

// Verify checks req.Signature over req.Data against the keys in req.PublicKey.
func (b *Binary) Verify(ctx context.Context, req VerifyRequest) error {
	gpgPath, err := b.resolve(ctx)
	if err != nil {
		return err
	}
	dir, cleanup, err := b.newHome(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if _, err := b.run(ctx, "import", gpgPath, homeArgs(dir, "--import"), req.PublicKey); err != nil {
		return err
	}

	sigPath := filepath.Join(dir, "sig.asc")
	if err := os.WriteFile(sigPath, []byte(req.Signature), 0o600); err != nil {
		return fmt.Errorf("write signature: %w", err)
	}
	digestPath := filepath.Join(dir, "digest.bin")
	if err := os.WriteFile(digestPath, req.Data, 0o600); err != nil {
		return fmt.Errorf("write signed data: %w", err)
	}

	args := homeArgs(dir, "--status-fd", "1", "--trust-model", "always", "--verify", sigPath, digestPath)
	out, err := b.run(ctx, "verify", gpgPath, args, nil)
	if err != nil {
		return fmt.Errorf("%w\nstatus: %s", err, strings.TrimSpace(string(out)))
	}
	fields, err := parseVerifyStatus(string(out))
	if err != nil {
		return err
	}

	want := req.KeyFingerprint
	if want == "" {
		return nil
	}
	signingFpr, primaryFpr := fields[0], fields[0]
	if len(fields) >= 10 {
		primaryFpr = fields[9]
	}
	if fingerprintMatches(signingFpr, want) || fingerprintMatches(primaryFpr, want) {
		return nil
	}
	return fmt.Errorf("signature was made by key %s (primary key %s), which does not match the configured key fingerprint %q",
		signingFpr, primaryFpr, want)
}

// homeArgs prefixes args with the options every gpg invocation on the isolated home directory needs.
func homeArgs(dir string, args ...string) []string {
	return append([]string{"--batch", "--no-tty", "--homedir", dir}, args...)
}

func (b *Binary) resolve(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.gpgPath != "" {
		return b.gpgPath, nil
	}

	path, err := b.LookPath("gpg")
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

	if b.gpgconfPath, err = b.LookPath("gpgconf"); err != nil {
		b.gpgconfPath = ""
		slog.DebugContext(ctx, "gpgconf not found on PATH; gpg-agent is not stopped explicitly after operations")
	}
	b.gpgPath = path
	return path, nil
}

// newHome creates an isolated GnuPG home directory. cleanup stops the daemons
// gpg started for it and removes it; it also runs after ctx is cancelled.
func (b *Binary) newHome(ctx context.Context) (string, func(), error) {
	dir, err := os.MkdirTemp("", "ocm-gpg-")
	if err != nil {
		return "", nil, fmt.Errorf("create temporary GnuPG home directory: %w", err)
	}
	cleanup := func() {
		if b.gpgconfPath != "" {
			kctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
			// "all" also covers keyboxd, which GnuPG >= 2.4 may start per home directory.
			if _, err := b.run(kctx, "kill", b.gpgconfPath, []string{"--homedir", dir, "--kill", "all"}, nil); err != nil {
				slog.DebugContext(ctx, "failed to stop gpg-agent", "homedir", dir, "error", err)
			}
			cancel()
		}
		if err := os.RemoveAll(dir); err != nil {
			slog.WarnContext(ctx, "failed to remove temporary GnuPG home directory containing key material", "path", dir, "error", err)
		}
	}
	return dir, cleanup, nil
}

// run invokes a gpg (or gpgconf) operation. stdout is returned even on error
// because verification needs the status lines to explain failures.
func (b *Binary) run(ctx context.Context, op, binaryPath string, args []string, stdin []byte) ([]byte, error) {
	timeout := b.OperationTimeout
	if timeout == 0 {
		timeout = defaultOperationTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// args never contain secrets: key material and the passphrase are passed on stdin.
	slog.DebugContext(ctx, "gpg: invoking", "operation", op, "args", args)

	stdout, stderr, err := b.Exec(ctx, binaryPath, args, stdin)
	if err != nil {
		msg := strings.TrimSpace(string(stderr))
		if len(msg) > maxStderr {
			msg = msg[:maxStderr]
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return stdout, fmt.Errorf("gpg %s timed out: %w\nstderr: %s", op, err, msg)
		}
		return stdout, fmt.Errorf("gpg %s failed: %w\nstderr: %s", op, err, msg)
	}
	return stdout, nil
}

func (b *Binary) exec(ctx context.Context, binaryPath string, args []string, stdin []byte) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = os.Environ()
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

// firstSecretKeyFingerprint returns the fingerprint of the first primary secret
// key in "gpg --list-secret-keys --with-colons" output.
func firstSecretKeyFingerprint(colons string) (string, error) {
	inSec := false
	scanner := bufio.NewScanner(strings.NewReader(colons))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "sec:"):
			inSec = true
		case strings.HasPrefix(line, "fpr:"):
			if inSec {
				if fields := strings.Split(line, ":"); len(fields) > 9 && fields[9] != "" {
					return fields[9], nil
				}
			}
		case strings.HasPrefix(line, "grp:"), strings.HasPrefix(line, "uid:"), strings.HasPrefix(line, "tru:"):
		default:
			// ssb, pub, sub, ... end the primary secret key block.
			inSec = false
		}
	}
	return "", errors.New("no secret key found in private key material")
}

// parseVerifyStatus requires GOODSIG and VALIDSIG status lines and returns the VALIDSIG fields.
// Field 1 is the signing key fingerprint, field 10 (if present) the primary key fingerprint.
func parseVerifyStatus(status string) ([]string, error) {
	var good bool
	var validFields []string
	for line := range strings.SplitSeq(status, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[GNUPG:] GOODSIG "):
			good = true
		case strings.HasPrefix(line, "[GNUPG:] VALIDSIG "):
			validFields = strings.Fields(strings.TrimPrefix(line, "[GNUPG:] VALIDSIG "))
		}
	}
	if !good || len(validFields) == 0 {
		return nil, fmt.Errorf("gpg reported no GOODSIG and VALIDSIG status\nstatus: %s", strings.TrimSpace(status))
	}
	return validFields, nil
}

// fingerprintMatches compares a fingerprint against a full fingerprint or a 16-hex long key ID.
func fingerprintMatches(fpr, want string) bool {
	if strings.EqualFold(fpr, want) {
		return true
	}
	return len(want) == 16 && strings.HasSuffix(strings.ToUpper(fpr), strings.ToUpper(want))
}
