package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Masterminds/semver/v3"
)

const defaultOperationTimeout = 3 * time.Minute

// cosignMinimumVersion is the minimum cosign version required on PATH.
// v3.0.4 introduced --signing-config which this handler depends on.
const cosignMinimumVersion = "v3.0.4"

//// Executor abstracts cosign CLI invocations for testability.
//// Callers must call Ensure before Run.
type Executor interface {
	Run(ctx context.Context, args []string, env []string) error
	Ensure(ctx context.Context) error
}

type cosignRunner interface {
	SignBlob(ctx context.Context, dataPath, bundlePath string, args, env []string) error
	VerifyBlob(ctx context.Context, dataPath, bundlePath string, args, env []string) error
}

// ErrNotResolved is returned by Run when Ensure has not been called.
var ErrNotResolved = errors.New("executor not resolved: call Ensure before Run")

//// DefaultExecutor invokes the cosign binary via os/exec.
//// Call Ensure to resolve the binary, then Run to execute commands.
type DefaultExecutor struct {
	mu         sync.Mutex
	binaryPath string
	resolved   atomic.Bool
	httpClient *http.Client
}

type cosignBinary struct {
	mu         sync.Mutex
	binaryPath string // empty until first successful resolve
	httpClient *http.Client
}

// ExecutorOption configures a DefaultExecutor.
type ExecutorOption func(*DefaultExecutor)

// WithHTTPClient sets the HTTP client used for cosign binary downloads.
func WithHTTPClient(c *http.Client) ExecutorOption {
	return func(e *DefaultExecutor) {
		e.httpClient = c
	}
}

// NewDefaultExecutor returns an executor that shells out to the cosign binary.
func NewDefaultExecutor(opts ...ExecutorOption) *DefaultExecutor {
	e := &DefaultExecutor{binaryPath: "cosign"}
	for _, opt := range opts {
		opt(e)
	}
	if e.httpClient == nil {
		e.httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	return e
}

// Ensure resolves the cosign binary (PATH lookup or auto-download).
// Must be called before Run. Safe to call multiple times (no-op after first success).
func (e *DefaultExecutor) Ensure(ctx context.Context) error {
	return e.ensureCosignAvailable(ctx)
}

func (e *DefaultExecutor) ensureCosignAvailable(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.resolved.Load() {
		return nil
	}
	path, err := exec.LookPath(e.binaryPath)
	if err == nil {
		if verr := e.checkVersion(ctx, path); verr != nil {
			return verr
		}
		e.binaryPath = path
		e.resolved.Store(true)
		return nil
	}
	path, dlErr := ensureOrDownloadCosign(ctx, e.httpClient)
	if dlErr != nil {
		return fmt.Errorf(
			"cosign binary not found on PATH and auto-download failed: %w "+
				"(install cosign from https://github.com/sigstore/cosign?tab=readme-ov-file#installation "+
				"and ensure it is on PATH, or fix the download error)", dlErr)
	}
	e.binaryPath = path
	e.resolved.Store(true)
	return nil
}

// Run executes the cosign binary with the given arguments and environment.
// Ensure must be called before Run; returns ErrNotResolved otherwise.
func (e *DefaultExecutor) Run(ctx context.Context, args []string, env []string) error {
	if !e.resolved.Load() {
		return ErrNotResolved
	}
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, e.binaryPath, args...) //nolint:gosec // G204: args are constructed from trusted config, not user input
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		const maxStderr = 4096
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > maxStderr {
			msg = msg[:maxStderr]
		}
		msg = scrubStderr(msg)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("cosign %s timed out: %w\nstderr: %s", args[0], err, msg)
		}
		return fmt.Errorf("cosign %s failed: %w\nstderr: %s", args[0], err, msg)
	}
	if out := strings.TrimSpace(stdout.String()); out != "" {
		slog.Debug("cosign output", "subcommand", args[0], "stdout", out)
	}
	return nil
}

// cosignEnv returns the full process environment — COSIGN_*, SIGSTORE_*, TUF_*, proxy vars all pass through.
func cosignEnv() []string {
	return os.Environ()
}

func hasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) && len(kv) > len(prefix) {
			return true
		}
	}
	return false
}

var versionRegexp = regexp.MustCompile(`v\d+\.\d+\.\d+`)

func (e *DefaultExecutor) checkVersion(ctx context.Context, binaryPath string) error {
	detected, err := detectCosignVersion(ctx, binaryPath)
	if err != nil {
		slog.Warn("could not determine cosign version on PATH; if signing fails, "+
			"verify cosign is >= "+cosignMinimumVersion+" or remove it from PATH to trigger auto-download",
			"path", binaryPath, "error", err)
		return nil
	}
	detectedVer, err := semver.NewVersion(detected)
	if err != nil {
		slog.Warn("could not parse cosign version; if signing fails, "+
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
		slog.Warn("cosign on PATH is older than the tested/pinned version; consider upgrading",
			"path", binaryPath, "path_version", detected, "pinned_version", cosignVersion)
	}
	return nil
}

func detectCosignVersion(ctx context.Context, binaryPath string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, binaryPath, "version")
	cmd.Stdout = &stdout
	cmd.Stderr = nil
	cmd.Env = cosignEnv()
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
	if m := versionRegexp.FindString(output); m != "" {
		return m, nil
	}
	return "", errors.New("could not parse cosign version from output")
}
