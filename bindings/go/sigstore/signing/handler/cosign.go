package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// defaultOperationTimeout is the maximum duration for a single cosign invocation
// when the caller's context has no deadline.
const defaultOperationTimeout = 3 * time.Minute

// Executor abstracts cosign CLI invocations for testability.
// Implementations receive raw data as byte slices and are responsible for
// transferring it to the cosign process (e.g. via temp files or stdin).
type Executor interface {
	SignData(ctx context.Context, data []byte, opts SignOpts) (bundleJSON []byte, err error)
	VerifyData(ctx context.Context, data []byte, bundlePath string, opts VerifyOpts) error
}

// SignOpts contains the options for a cosign sign-blob invocation.
type SignOpts struct {
	IdentityToken      string
	SigningConfig      string
	FulcioURL          string
	RekorURL           string
	TimestampServerURL string
	TrustedRoot        string
}

// VerifyOpts contains the options for a cosign verify-blob invocation.
type VerifyOpts struct {
	CertificateIdentity         string
	CertificateIdentityRegexp   string
	CertificateOIDCIssuer       string
	CertificateOIDCIssuerRegexp string
	TrustedRoot                 string
	PrivateInfrastructure       bool
}

// DefaultExecutor invokes the cosign binary via os/exec.
//
// binaryPath starts as "cosign" and is resolved to an absolute path inside
// checkOnce.Do. All reads of binaryPath happen after Do completes (which acts
// as a memory barrier), so no additional synchronisation is needed.
type DefaultExecutor struct {
	binaryPath string

	checkOnce sync.Once
	checkErr  error
}

// NewDefaultExecutor returns an executor that shells out to the cosign binary.
// It first looks for cosign on PATH, then falls back to a cached or freshly
// downloaded version.
func NewDefaultExecutor() (*DefaultExecutor, error) {
	e := &DefaultExecutor{binaryPath: "cosign"}
	if err := e.ensureCosignAvailable(); err != nil {
		return nil, err
	}
	return e, nil
}

// ensureCosignAvailable resolves the cosign binary path.
// It first checks PATH, then falls back to downloading a pinned version.
//
// The resolution runs at most once per DefaultExecutor instance via sync.Once.
// If the download fails, the error is permanently cached — subsequent calls
// return the same error without retrying. To retry after a transient failure,
// create a new DefaultExecutor (and therefore a new Handler via New()).
func (e *DefaultExecutor) ensureCosignAvailable() error {
	e.checkOnce.Do(func() {
		path, err := exec.LookPath(e.binaryPath)
		if err == nil {
			e.binaryPath = path
			return
		}

		path, dlErr := ensureOrDownloadCosign()
		if dlErr != nil {
			e.checkErr = fmt.Errorf(
				"cosign binary not found on PATH and auto-download failed: %w "+
					"(install cosign from https://github.com/sigstore/cosign?tab=readme-ov-file#installation "+
					"and ensure it is on PATH, or fix the download error)", dlErr)
			return
		}
		e.binaryPath = path
	})
	return e.checkErr
}

// writeTemp creates a temp file in dir (empty string = OS default) with the given
// pattern, writes data to it, closes it, and returns the path.
// The caller must remove the file when done.
func writeTemp(dir, pattern string, data []byte) (path string, err error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create temp file %q: %w", pattern, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close temp file %q: %w", pattern, cerr)
		}
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(data); err != nil {
		return "", fmt.Errorf("write temp file %q: %w", pattern, err)
	}
	return f.Name(), nil
}

// cosignPassthroughEnvKeys lists environment variable keys forwarded to the cosign subprocess.
var cosignPassthroughEnvKeys = map[string]bool{
	"PATH":               true,
	"HOME":               true,
	"TMPDIR":             true,
	"TMP":                true,
	"TEMP":               true,
	"USERPROFILE":        true,
	"HTTP_PROXY":         true,
	"HTTPS_PROXY":        true,
	"NO_PROXY":           true,
	"http_proxy":         true,
	"https_proxy":        true,
	"no_proxy":           true,
	"SSL_CERT_FILE":      true,
	"SSL_CERT_DIR":       true,
	"REQUESTS_CA_BUNDLE": true,
}

// cosignEnv builds a minimal environment for the cosign subprocess.
// It forwards PATH, HOME, TMPDIR/TMP/TEMP (platform-specific), proxy vars
// (HTTP_PROXY, HTTPS_PROXY, NO_PROXY and lowercase variants), and TLS CA vars
// (SSL_CERT_FILE, SSL_CERT_DIR, REQUESTS_CA_BUNDLE).
// Keys listed in exclude are skipped — callers use this to prevent duplicates
// when they append an explicit value (e.g. SIGSTORE_ID_TOKEN).
//
// All signing/verification configuration is passed via explicit CLI flags
// derived from OCM typed config. No COSIGN_* or SIGSTORE_* env vars are
// forwarded from the parent process.
//
// Using a curated environment rather than os.Environ() avoids inadvertently
// forwarding sensitive parent-process secrets (DB passwords, cloud credentials, etc.)
// to the cosign subprocess.
func cosignEnv(exclude ...string) []string {
	skip := make(map[string]bool, len(exclude))
	for _, k := range exclude {
		skip[k] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if skip[key] {
			continue
		}
		if cosignPassthroughEnvKeys[key] {
			env = append(env, kv)
		}
	}
	return env
}

func (e *DefaultExecutor) SignData(ctx context.Context, data []byte, opts SignOpts) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "cosign-sign-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir for sign: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dataPath, err := writeTemp(tmpDir, "data-*", data)
	if err != nil {
		return nil, fmt.Errorf("write sign data to temp file: %w", err)
	}

	bundlePath, err := writeTemp(tmpDir, "bundle-*.json", nil)
	if err != nil {
		return nil, fmt.Errorf("create empty bundle temp file: %w", err)
	}

	args := []string{
		"sign-blob", dataPath,
		"--bundle", bundlePath,
		"--yes",
	}
	// Endpoint discovery precedence:
	//   1. --signing-config: cosign discovers Fulcio/Rekor/TSA from the file.
	//   2. Explicit --fulcio-url etc.: passed directly, disables signing-config auto-discovery.
	//   3. Neither: cosign's default TUF-based discovery.
	if opts.SigningConfig != "" {
		args = append(args, "--signing-config", opts.SigningConfig)
	} else if opts.FulcioURL != "" || opts.RekorURL != "" || opts.TimestampServerURL != "" {
		args = append(args, "--use-signing-config=false")
	}
	if opts.FulcioURL != "" {
		args = append(args, "--fulcio-url", opts.FulcioURL)
	}
	if opts.RekorURL != "" {
		args = append(args, "--rekor-url", opts.RekorURL)
	}
	if opts.TimestampServerURL != "" {
		args = append(args, "--timestamp-server-url", opts.TimestampServerURL)
	}
	if opts.TrustedRoot != "" {
		args = append(args, "--trusted-root", opts.TrustedRoot)
	}

	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, e.binaryPath, args...) //nolint:gosec // G204: args are constructed from trusted config, not user input
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr

	cmd.Env = append(cosignEnv("SIGSTORE_ID_TOKEN"), "SIGSTORE_ID_TOKEN="+opts.IdentityToken)

	if err := cmd.Run(); err != nil {
		const maxStderr = 4096
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > maxStderr {
			msg = msg[:maxStderr]
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("cosign sign-blob timed out: %w\nstderr: %s", err, msg)
		}
		return nil, fmt.Errorf("cosign sign-blob failed: %w\nstderr: %s", err, msg)
	}

	bundleJSON, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("read bundle output: %w", err)
	}

	return bundleJSON, nil
}

func (e *DefaultExecutor) VerifyData(ctx context.Context, data []byte, bundlePath string, opts VerifyOpts) error {
	tmpDir, err := os.MkdirTemp("", "cosign-verify-*")
	if err != nil {
		return fmt.Errorf("create temp dir for verify: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dataPath, err := writeTemp(tmpDir, "data-*", data)
	if err != nil {
		return err
	}

	args := []string{"verify-blob", dataPath, "--bundle", bundlePath}

	if opts.CertificateIdentity != "" {
		args = append(args, "--certificate-identity", opts.CertificateIdentity)
	}
	if opts.CertificateIdentityRegexp != "" {
		args = append(args, "--certificate-identity-regexp", opts.CertificateIdentityRegexp)
	}
	if opts.CertificateOIDCIssuer != "" {
		args = append(args, "--certificate-oidc-issuer", opts.CertificateOIDCIssuer)
	}
	if opts.CertificateOIDCIssuerRegexp != "" {
		args = append(args, "--certificate-oidc-issuer-regexp", opts.CertificateOIDCIssuerRegexp)
	}
	if opts.TrustedRoot != "" {
		args = append(args, "--trusted-root", opts.TrustedRoot)
	}
	if opts.PrivateInfrastructure {
		args = append(args, "--private-infrastructure")
	}

	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, e.binaryPath, args...) //nolint:gosec // G204: args are constructed from trusted config, not user input
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr
	cmd.Env = cosignEnv()

	if err := cmd.Run(); err != nil {
		const maxStderr = 4096
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > maxStderr {
			msg = msg[:maxStderr]
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("cosign verify-blob timed out: %w\nstderr: %s", err, msg)
		}
		return fmt.Errorf("cosign verify-blob failed: %w\nstderr: %s", err, msg)
	}

	return nil
}
