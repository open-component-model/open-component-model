package internal

import (
	"bytes"
	"context"
	"crypto/fips140"
	"debug/buildinfo"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
)

const (
	defaultOperationTimeout  = 3 * time.Minute
	defaultHTTPClientTimeout = 5 * time.Minute
	cosignMinimumVersion     = "v3.0.4"
)

// CosignBinary resolves and invokes the cosign binary.
// Resolution is attempted on every call until it succeeds once; after that the
// resolved path is reused without locking. This makes resolution safe to retry
// in controller reconcile loops where transient failures must not be permanent.
type CosignBinary struct {
	mu               sync.Mutex
	binaryPath       string // non-empty after first successful resolution
	HttpClient       *http.Client
	OperationTimeout time.Duration                                                          // zero means use defaultOperationTimeout
	ExecCosign       func(ctx context.Context, binaryPath string, args, env []string) error // runs a cosign subcommand; args[0] is the subcommand name
	LookPath         func(file string) (string, error)                                      // locates the cosign binary on PATH
	FIPSEnabled      func() bool                                                            // reports FIPS 140-3 mode (fips140=on or only); a non-FIPS cosign is then logged
	FIPSEnforced     func() bool                                                            // reports strict FIPS 140-3 mode (fips140=only); a non-FIPS cosign is then rejected
	ReadBuildInfo    func(path string) (*buildinfo.BuildInfo, error)                        // reads the Go build information of a binary
}

func NewCosignBinary() *CosignBinary {
	b := &CosignBinary{
		HttpClient:    &http.Client{Timeout: defaultHTTPClientTimeout},
		FIPSEnabled:   fips140.Enabled,
		FIPSEnforced:  fips140.Enforced,
		ReadBuildInfo: buildinfo.ReadFile,
	}
	b.ExecCosign = b.execCosign
	b.LookPath = exec.LookPath
	return b
}

// Sign invokes "cosign sign-blob" on dataPath and writes the resulting Sigstore bundle to bundlePath.
// Resolution of the cosign binary is attempted lazily and cached after first success.
func (b *CosignBinary) Sign(ctx context.Context, dataPath, bundlePath string, extraArgs, env []string) error {
	path, err := b.resolveBinary(ctx)
	if err != nil {
		return fmt.Errorf("resolve cosign binary: %w", err)
	}
	args := make([]string, 0, 5+len(extraArgs))
	args = append(args, "sign-blob", dataPath, "--bundle", bundlePath, "--yes")
	args = append(args, extraArgs...)
	return b.ExecCosign(ctx, path, args, env)
}

// Verify invokes "cosign verify-blob" on dataPath using the Sigstore bundle at bundlePath.
// Identity and issuer constraints are passed via extraArgs by the caller.
func (b *CosignBinary) Verify(ctx context.Context, dataPath, bundlePath string, extraArgs, env []string) error {
	path, err := b.resolveBinary(ctx)
	if err != nil {
		return fmt.Errorf("resolve cosign binary: %w", err)
	}
	args := make([]string, 0, 4+len(extraArgs))
	args = append(args, "verify-blob", dataPath, "--bundle", bundlePath)
	args = append(args, extraArgs...)
	return b.ExecCosign(ctx, path, args, env)
}

// ErrCosignDownloadInFIPSMode is returned when no cosign is on PATH with GODEBUG=fips140=only. OCM then neither
// downloads the upstream release nor uses a previously downloaded one, because those are not FIPS builds.
var ErrCosignDownloadInFIPSMode = errors.New("cosign binary not found on PATH; with GODEBUG=fips140=only, downloading cosign is disabled " +
	"because the upstream release is not a FIPS build: install a cosign built with GOFIPS140 and ensure it is on PATH")

// ErrCosignNotFIPSBuild is returned with GODEBUG=fips140=only when the cosign on PATH is not built against a
// frozen Go Cryptographic Module, because Sigstore signing would then leave the FIPS boundary.
var ErrCosignNotFIPSBuild = errors.New("with GODEBUG=fips140=only, Sigstore signing requires a cosign built against a frozen " +
	"Go Cryptographic Module (GOFIPS140=v<version>, see go version -m)")

// resolveBinary locates cosign. In FIPS 140-3 mode it checks that cosign is a FIPS build: with fips140=only a
// non-FIPS cosign (or downloading one) is rejected, with fips140=on it is used and logged at debug level.
func (b *CosignBinary) resolveBinary(ctx context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.binaryPath != "" {
		return b.binaryPath, nil
	}
	fipsMode := b.FIPSEnabled != nil && b.FIPSEnabled()
	enforced := b.FIPSEnforced != nil && b.FIPSEnforced()
	path, err := b.LookPath("cosign")
	if err == nil {
		if verr := b.ensureMinimumVersion(ctx, path); verr != nil {
			return "", verr
		}
		if fipsMode || enforced {
			if ferr := b.requireFIPSBuild(path); ferr != nil {
				if enforced {
					return "", ferr
				}
				slog.DebugContext(ctx, "cosign is not a FIPS build; Sigstore signing runs outside the FIPS 140-3 boundary "+
					"(GODEBUG=fips140=only rejects it)", "path", path, "reason", ferr.Error())
			}
		}
		b.binaryPath = path
		return path, nil
	}
	if enforced {
		return "", ErrCosignDownloadInFIPSMode
	}
	if fipsMode {
		slog.DebugContext(ctx, "no cosign on PATH; using the upstream release, which is not a FIPS build, so Sigstore signing "+
			"runs outside the FIPS 140-3 boundary (GODEBUG=fips140=only rejects it)")
	}
	path, dlErr := ensureOrDownloadCosign(ctx, b.HttpClient)
	if dlErr != nil {
		return "", fmt.Errorf(
			"cosign binary not found on PATH and auto-download failed: %w "+
				"(install cosign from https://github.com/sigstore/cosign?tab=readme-ov-file#installation "+
				"and ensure it is on PATH, or fix the download error)", dlErr)
	}
	b.binaryPath = path
	return path, nil
}

// requireFIPSBuild checks the Go build information of the cosign binary for a GOFIPS140 setting that names a
// frozen module version (v<version>). "latest" or no setting means cosign does not use a frozen module.
func (b *CosignBinary) requireFIPSBuild(path string) error {
	info, err := b.ReadBuildInfo(path)
	if err != nil {
		return fmt.Errorf("%w: reading the Go build information of %s failed: %w", ErrCosignNotFIPSBuild, path, err)
	}
	for _, s := range info.Settings {
		if s.Key == "GOFIPS140" {
			if strings.HasPrefix(s.Value, "v") {
				return nil
			}
			return fmt.Errorf("%w: %s was built with GOFIPS140=%s", ErrCosignNotFIPSBuild, path, s.Value)
		}
	}
	return fmt.Errorf("%w: %s was built without GOFIPS140", ErrCosignNotFIPSBuild, path)
}

func (b *CosignBinary) execCosign(ctx context.Context, binaryPath string, args, env []string) error {
	timeout := b.OperationTimeout
	if timeout == 0 {
		timeout = defaultOperationTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// args[0] is the subcommand (sign-blob/verify-blob); the remaining args are file paths and
	// configuration flags such as --certificate-identity. None of these contain secrets — the OIDC
	// token is passed through env (SIGSTORE_ID_TOKEN), never argv.
	slog.DebugContext(ctx, "cosign: invoking subcommand", "subcommand", args[0], "args", args[1:])

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		// Truncate stderr to avoid unbounded error messages in logs and wrapped errors.
		const maxStderr = 4096
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > maxStderr {
			msg = msg[:maxStderr]
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("cosign %s timed out: %w\nstderr: %s", args[0], err, msg)
		}
		return fmt.Errorf("cosign %s failed: %w\nstderr: %s", args[0], err, msg)
	}
	if out := strings.TrimSpace(stdout.String()); out != "" {
		slog.DebugContext(ctx, "cosign output", "subcommand", args[0], "stdout", out)
	}
	if errOut := strings.TrimSpace(stderr.String()); errOut != "" {
		slog.DebugContext(ctx, "cosign output", "subcommand", args[0], "stderr", errOut)
	}
	return nil
}

func (b *CosignBinary) ensureMinimumVersion(ctx context.Context, binaryPath string) error {
	detected, err := detectCosignVersion(ctx, binaryPath)
	if err != nil {
		slog.WarnContext(ctx, "could not determine cosign version on PATH; if signing fails, "+
			"verify cosign is >= "+cosignMinimumVersion+" or remove it from PATH to trigger auto-download",
			"path", binaryPath, "error", err)
		return nil
	}
	detectedVer, err := semver.NewVersion(detected)
	if err != nil {
		slog.WarnContext(ctx, "could not parse cosign version; if signing fails, "+
			"verify cosign is >= "+cosignMinimumVersion+" or remove it from PATH to trigger auto-download",
			"detected", detected, "error", err)
		return nil
	}
	minimumVer, err := semver.NewVersion(cosignMinimumVersion)
	if err != nil {
		return fmt.Errorf("parse minimum version constant: %w", err)
	}
	if detectedVer.LessThan(minimumVer) {
		return fmt.Errorf(
			"cosign on PATH (%s) is version %s, minimum required is %s "+
				"(--signing-config flag not available in older versions)",
			binaryPath, detected, cosignMinimumVersion)
	}
	pinnedVer, _ := semver.NewVersion(cosignVersion)
	if pinnedVer != nil && detectedVer.LessThan(pinnedVer) {
		slog.WarnContext(ctx, "cosign on PATH is older than the tested/pinned version; consider upgrading",
			"path", binaryPath, "path_version", detected, "pinned_version", cosignVersion)
	}
	return nil
}

func detectCosignVersion(ctx context.Context, binaryPath string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, binaryPath, "version")
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run cosign version: %w", err)
	}
	return parseCosignVersionOutput(stdout.String())
}

func parseCosignVersionOutput(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "GitVersion:"); ok {
			return strings.TrimSpace(v), nil
		}
	}
	versionRegexp := regexp.MustCompile(`v\d+\.\d+\.\d+`)
	if m := versionRegexp.FindString(output); m != "" {
		return m, nil
	}
	return "", errors.New("could not parse cosign version from output")
}

func HasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) && len(kv) > len(prefix) {
			return true
		}
	}
	return false
}
